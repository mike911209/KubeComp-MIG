package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kv1 "knative.dev/serving/pkg/apis/serving/v1"
)

const (
	TGI_REQUEST_METRIC       = "tgi_request_mean_time_per_token_duration"
	TGI_REQUEST_METRIC_COUNT = "tgi_request_mean_time_per_token_duration_count"
)
const (
	MIG_1G = iota
	MIG_2G
	MIG_3G
	MIG_4G
	MIG_7G
)
const (
	defaultScrapeInterval = 10 * time.Second
	defaultNamespace      = "default"
	defaultExportInterval = 1 * time.Second // should be less than scrape interval, otherwise, scaling will be delayed due to the exporter
	scalingLabel          = "auto-scaler"
)

var (
	migConfigs = []string{
		"nvidia.com/mig-1g.5gb",
		"nvidia.com/mig-2g.10gb",
		"nvidia.com/mig-3g.20gb",
		"nvidia.com/mig-4g.20gb",
		"nvidia.com/mig-7g.40gb",
	}
	migConfigsIdx = map[string]int{
		"nvidia.com/mig-1g.5gb":  MIG_1G,
		"nvidia.com/mig-2g.10gb": MIG_2G,
		"nvidia.com/mig-3g.20gb": MIG_3G,
		"nvidia.com/mig-4g.20gb": MIG_4G,
		"nvidia.com/mig-7g.40gb": MIG_7G,
	}
)

type Autoscaler struct {
	config        Config
	kubeClient    *kubernetes.Clientset
	exporter      *Exporter
	knativeHelper *KnativeHelper
	scaler        Scaler // interface
	// fetcher       *MetricFetcher // interface
	ignoreList []string
}
type Config struct {
	PrometheusURL  string
	ScrapeInterval time.Duration
	Namespace      string
	ignoreList     []string
}
type RevisionData struct {
	name      string
	namespace string
	svcName   string
	metrics   map[string]float64
	gpuName   string
	usingMig  bool
	slo       float64
}

// TODO: add more resource
type NodeResources struct {
	NodeName  string
	migSlices map[string]int
}

func NewAutoscaler(cfg Config, kubeClient *kubernetes.Clientset, exporter *Exporter, knativeHelper *KnativeHelper, scaler Scaler) *Autoscaler {
	return &Autoscaler{
		config:        cfg,
		kubeClient:    kubeClient,
		exporter:      exporter,
		knativeHelper: knativeHelper,
		scaler:        scaler,
		// fetcher:       fetcher,
		ignoreList: cfg.ignoreList,
	}
}

func (a *Autoscaler) getNodeResources(nodeName string) (NodeResources, error) {
	// TODO: add MPS and another GPU (resources) support
	node, err := a.kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return NodeResources{}, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	migSlices := make(map[string]int)
	for _, migConfig := range migConfigs {
		rQuant := node.Status.Allocatable[v1.ResourceName(migConfig)]
		migSlices[migConfig] = int(rQuant.Value())
	}

	return NodeResources{
		NodeName:  nodeName,
		migSlices: migSlices,
	}, nil
}

func getCurrentGPU(pod v1.Pod) (string, bool, error) {
	// TODO: add MPS support
	for _, migConfig := range migConfigs {
		rQuant := pod.Spec.Containers[0].Resources.Requests[v1.ResourceName(migConfig)]
		if rQuant.Value() > 0 {
			return migConfig, true, nil
		}
	}
	return "", false, fmt.Errorf("pod %s is not using any GPU", pod.Name)
}

func (a *Autoscaler) getRevisionData(pod v1.Pod) (RevisionData, error) {
	gpuName, usingMig, err := getCurrentGPU(pod)
	if err != nil {
		return RevisionData{}, fmt.Errorf("Error getting GPU for pod %s: %v", pod.Name, err)
	}

	slo, err := strconv.ParseFloat(pod.Labels["slo"], 64)
	if err != nil {
		return RevisionData{}, fmt.Errorf("failed to parse SLO for pod %s: %w", pod.Name, err)
	}

	return RevisionData{
		name:      pod.Labels["serving.knative.dev/revision"],
		namespace: pod.Namespace,
		svcName:   pod.Labels["serving.knative.dev/service"],
		metrics:   nil,
		gpuName:   gpuName,
		usingMig:  usingMig,
		slo:       slo,
	}, nil
}

// TODO: prevent indefinitely creating new revision, maybe add a limit, if the limit is reached, stop processing the services
func (a *Autoscaler) ProcessService(serviceName string) error {
	pods, err := a.kubeClient.CoreV1().Pods(a.config.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("serving.knative.dev/service=%s", serviceName),
	})
	if err != nil {
		return fmt.Errorf("Failed to list pods for service %s: %w", serviceName, err)
	}

	for _, pod := range pods.Items {
		if err = a.processPod(serviceName, pod); err != nil {
			log.Printf("Error processing pods %s: %v", pod.Name, err)
			continue
		}
	}
	return nil
}

func (a *Autoscaler) processPod(serviceName string, pod v1.Pod) error {
	// TODO: label pod for scaling
	// Step 1: get revision data
	revisionData, err := a.getRevisionData(pod)
	if err != nil {
		return fmt.Errorf("failed to get revision data for pod %s: %w", pod.Name, err)
	}

	// register (update if exist) gpu resource in prometheus with exporter
	a.exporter.SendScalingEvent(revisionData, NotScaling)

	// Step 2: Check if pod is running
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			return fmt.Errorf("pod %s is not ready", pod.Name)
		}
	}
	if pod.Status.Phase != v1.PodRunning || pod.DeletionTimestamp != nil {
		return fmt.Errorf("pod %s is not running", pod.Name)
	}

	// Step 3: Get node resources
	nodeResources, err := a.getNodeResources(pod.Spec.NodeName)
	if err != nil {
		return fmt.Errorf("failed to get node resources for pod %s: %w", pod.Name, err)
	}

	// Step 4: Obtain metrics from Prometheus
	// TODO finish fetch metrics
	// metrics, err := a.fetcher.FetchPodMetrics(pod.Name)
	// if err != nil {
	// a.sendEvent(revisionData.name, int(NotScaling), revisionData.gpuName)
	// 	return fmt.Errorf("failed to fetch metrics for pod %s: %w", pod.Name, err)
	// }
	revisionData.metrics = (map[string]float64{
		TGI_REQUEST_METRIC: 1,
	})

	// Step 5: Decide scaling
	scaleDecision, resourceRequirements := a.scaler.DecideScale(revisionData, nodeResources)
	if scaleDecision != NotScaling {
		if err := a.scaler.ApplyScale(scaleDecision, revisionData, resourceRequirements, a.knativeHelper); err != nil {
			return fmt.Errorf("failed to apply scaling decision for pod %s: %w", pod.Name, err)
		}
	}

	// update new gpu resource to prometheus after scaling
	a.exporter.SendScalingEvent(revisionData, scaleDecision)
	return nil
}

func (a *Autoscaler) shouldProcessService(service kv1.Service) bool {
	name := service.Name
	for _, ignore := range a.ignoreList {
		if strings.Contains(name, ignore) {
			return false
		}
	}
	return service.Labels[scalingLabel] != "scaling"
}

// // labelScaling labels the service that is being scaling, preventing it from being processed again
// func (a *Autoscaler) labelScaling(serviceName string) {
// 	client, err := a.createKnativeClient()
// 	if err != nil {
// 		log.Fatalf("Error creating Knative serving client: %s", err.Error())
// 	}

// 	// Get the existing service instance
// 	oldService, err := client.GetService(context.TODO(), serviceName)
// 	if err != nil {
// 		log.Fatalf("Failed to get Knative service: %v", err)
// 		return
// 	}

// 	// create empty service instance
// 	// copy spec of unscaled service to new service
// 	newService := oldService.DeepCopy()
// 	if newService.Labels == nil {
// 		newService.Labels = make(map[string]string)
// 	}
// 	newService.Labels["auto-scaler"] = "scaling"

// 	//Create the new service with the updated spec
// 	ctx := context.Background()
// 	log.Printf("Update Knative service: %s's label", serviceName)
// 	_, err = client.UpdateService(ctx, newService)
// 	if err != nil {
// 		log.Printf("Error creating Knative service: %s", err.Error())
// 	}

// 	log.Printf("Service %s labeled for scaling", serviceName)
// }

func parseConfig() (Config, error) {
	var interval time.Duration
	var err error
	interval_ := os.Getenv("METRICS_SCRAPE_INTERVAL")
	if interval_ != "" {
		interval, err = time.ParseDuration(interval_ + "s")
		if err != nil {
			log.Printf("Invalid scrape interval: %v, use default scrape interval: %d", err, int(defaultScrapeInterval.Seconds()))
			interval = defaultScrapeInterval
		}
	} else {
		interval = defaultScrapeInterval
	}

	namespace := os.Getenv("NAMESPACE")
	if namespace == "" {
		namespace = defaultNamespace
	}

	var ignoreList []string
	ignoreList_ := os.Getenv("IGNORE_LIST")
	if ignoreList_ != "" {
		ignoreList = strings.Split(ignoreList_, ",")
	}

	return Config{
		PrometheusURL:  "http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090/api/v1/query",
		ScrapeInterval: interval,
		Namespace:      namespace,
		ignoreList:     ignoreList,
	}, nil
}

func main() {
	log.Println("Starting Autoscaler...")

	autoscalerCfg, err := parseConfig()
	if err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to get Kubernetes config: %v", err)
	}
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	exporter := NewExporter(defaultExportInterval)
	knativeHelper := NewKnativeHelper(autoscalerCfg.Namespace)
	scaler := NewSimpleScaler()
	// fetcher := NewSimpleFetcher()
	autoscaler := NewAutoscaler(autoscalerCfg, kubeClient, exporter, knativeHelper, scaler)

	autoscaler.exporter.StartExporter()

	ticker := time.NewTicker(autoscalerCfg.ScrapeInterval)
	defer ticker.Stop()
	for {
		<-ticker.C
		services, err := autoscaler.knativeHelper.ListServices(context.TODO())
		if err != nil {
			log.Printf("Failed to list Knative services in namespace %s: %v", autoscalerCfg.Namespace, err)
			continue
		}
		for _, service := range services.Items {
			if autoscaler.shouldProcessService(service) {
				log.Printf("\n\nProcessing Knative service: %s\n", service.Name)
				if err := autoscaler.ProcessService(service.Name); err != nil {
					log.Printf("Error processing Knative service %s: %v", service.Name, err)
				}
			}
		}
	}
}
