package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"knative.dev/client/pkg/commands"
	serving "knative.dev/client/pkg/serving/v1"
	kv1 "knative.dev/serving/pkg/apis/serving/v1"
)

const prometheusURL = "http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090/api/v1/query"
const (
	TGI_REQUEST_METRIC       = "tgi_request_mean_time_per_token_duration"
	TGI_REQUEST_METRIC_COUNT = "tgi_request_mean_time_per_token_duration_count"
)
const (
	NOT_SCALING = iota
	SCALING_UP
	SCALING_DOWN
	SCALING_IN
)
const (
	MIG_1G = iota
	MIG_2G
	MIG_3G
	MIG_4G
	MIG_7G
)

type PodData struct {
	podName            string
	currentGPU         int64 // 0 for smallest, 4 for biggest
	TGI_REQUEST_METRIC float64
}
type NodeResources struct {
	NodeName  string
	migSlices map[string]int
}
type ResourceSpec struct {
	podName     string
	scalingType int
	cpu         string
	memory      string
	migSlice    map[string]int
	slo         float64
}
type Autoscaler struct {
	Clientset          *kubernetes.Clientset
	ScrapeInterval     time.Duration
	scaleUpThreshold   float64
	scaleDownThreshold float64
}
type Event struct {
	revisionName string
	scalingType  int // SCALING_UP, SCALING_DOWN, ...
	resourceName string
}

var (
	migConfigs = []string{
		"nvidia.com/mig-1g.5gb",
		"nvidia.com/mig-2g.10gb",
		"nvidia.com/mig-3g.20gb",
		"nvidia.com/mig-4g.20gb",
		"nvidia.com/mig-7g.40gb",
	}
	EventsChan = make(chan Event)
)

func NewAutoscaler() (*Autoscaler, error) {
	_scaleUpThreshold := os.Getenv("SCALE_UP_THRESHOLD")
	if _scaleUpThreshold == "" {
		_scaleUpThreshold = "1.5"
	}
	scaleUpThreshold, _ := strconv.ParseFloat(_scaleUpThreshold, 64)

	_scaleDownThreshold := os.Getenv("SCALE_DOWN_THRESHOLD")
	if _scaleDownThreshold == "" {
		_scaleDownThreshold = "0.5"
	}
	scaleDownThreshold, _ := strconv.ParseFloat(_scaleDownThreshold, 64)

	interval_ := os.Getenv("METRICS_SCRAPE_INTERVAL")
	if interval_ == "" {
		interval_ = "10"
	}
	interval, err := time.ParseDuration(interval_ + "s")
	if err != nil {
		return nil, fmt.Errorf("invalid scrape interval: %v", err)
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return &Autoscaler{
		Clientset:          clientset,
		ScrapeInterval:     interval,
		scaleUpThreshold:   scaleUpThreshold,
		scaleDownThreshold: scaleDownThreshold,
	}, nil
}

func (a *Autoscaler) fetchPrometheusData(metric string, query string, podMetrics map[string]float64) error {
	resp, err := http.Get(fmt.Sprintf("%s?query=%s", prometheusURL, query))
	if err != nil {
		return fmt.Errorf("failed to fetch data from Prometheus: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	var result struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %v", err)
	}

	for _, res := range result.Data.Result {
		value, err := strconv.ParseFloat(res.Value[1].(string), 64)
		if err != nil {
			return fmt.Errorf("failed to parse value: %v", err)
		}
		podMetrics[metric] = value
	}

	return nil
}

func (a *Autoscaler) getNodeResources(nodeName string) (NodeResources, error) {
	node, err := a.Clientset.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Failed to get node: %v", err)
		return NodeResources{}, nil
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

func (a *Autoscaler) shouldScale(podData PodData, nodeResources NodeResources, SLO float64, notReceivingRequest bool) (int, ResourceSpec) {
	//TODO : define scaling decision logic
	log.Println("Checking if should scale - PodData:", podData, "NodeResources:", nodeResources, "SLO:", SLO)

	spec := ResourceSpec{
		podName:     podData.podName,
		scalingType: NOT_SCALING,
		cpu:         "1",
		memory:      "90Gi",
	}

	if podData.TGI_REQUEST_METRIC < SLO*a.scaleDownThreshold || notReceivingRequest {
		return a.scaleDown(podData, nodeResources, spec)
	} else if podData.TGI_REQUEST_METRIC > SLO*a.scaleUpThreshold {
		return a.scaleUp(podData, nodeResources, spec)
	}

	log.Println("No scaling needed")
	return NOT_SCALING, spec
}

func (a *Autoscaler) scaleDown(podData PodData, nodeResources NodeResources, spec ResourceSpec) (int, ResourceSpec) {
	if podData.currentGPU == MIG_1G {
		log.Println("Pod is already using the smallest MIG GPU")
		return NOT_SCALING, spec
	}

	if nodeResources.migSlices[migConfigs[podData.currentGPU-1]] > 0 {
		spec.migSlice = map[string]int{migConfigs[podData.currentGPU-1]: 1}
		log.Println("Scaling down")
		spec.scalingType = SCALING_DOWN
		return SCALING_DOWN, spec
	}

	log.Println("No MIG slices available for scaling down")
	return NOT_SCALING, spec
}

func (a *Autoscaler) scaleUp(podData PodData, nodeResources NodeResources, spec ResourceSpec) (int, ResourceSpec) {
	if podData.currentGPU == MIG_7G {
		log.Println("Pod is already using the biggest MIG GPU")
		return NOT_SCALING, spec
	}

	if nodeResources.migSlices[migConfigs[podData.currentGPU+1]] > 0 {
		spec.migSlice = map[string]int{migConfigs[podData.currentGPU+1]: 1}
		log.Println("Scaling up")
		spec.scalingType = SCALING_UP
		return SCALING_UP, spec
	}

	log.Println("No MIG slices available for scaling up")
	return NOT_SCALING, spec
}

func (a *Autoscaler) scaleKnativeService(serviceName string, spec ResourceSpec) {
	// TODO : deal with boundary cases:
	// 1. If the service has same spec --> dont scale
	// 3. Check if service state is ready
	// 4. Create thread to process scaling for a service

	log.Printf("Scaling Knative service - Name: %s", serviceName)

	// Create new knative serving client
	client, err := a.createKnativeClient()
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	// Get the existing service instance
	oldService, err := client.GetService(context.TODO(), serviceName)
	if err != nil {
		log.Fatalf("Failed to get Knative service: %v", err)
		return
	}

	// copy spec of unscaled service to new service
	newService := oldService.DeepCopy()
	a.updateServiceSpec(newService, spec)

	// Create the new service with the updated spec
	ctx := context.Background()
	log.Printf("Update Knative service: %s's resource spec", serviceName)
	// write slo to pod label
	if newService.Spec.Template.Labels == nil {
		newService.Spec.Template.Labels = make(map[string]string)
	}
	newService.Spec.Template.Labels["slo"] = fmt.Sprintf("%f", spec.slo)
	_, err = client.UpdateService(ctx, newService)
	if err != nil {
		log.Printf("Error creating Knative service: %s", err.Error())
	}

	a.waitForNewRevision(client, oldService)
	newService, err = client.GetService(context.TODO(), serviceName)
	if err != nil {
		log.Fatalf("Failed to get Knative service: %v", err)
		return
	}
	a.changeTrafficToNewRevision(client, newService)
	a.deleteOldRevision(client, oldService)

	newService, err = client.GetService(context.TODO(), serviceName)
	if err != nil {
		log.Fatalf("Failed to get Knative service: %v", err)
		return
	}
	a.labelServiceAsDone(client, newService)

	log.Println("Knative service scaled successfully")

	a.sendScalingEvent(newService, spec)
}

func (a *Autoscaler) updateServiceSpec(service *kv1.Service, spec ResourceSpec) {
	for i := range service.Spec.Template.Spec.Containers {
		container := &service.Spec.Template.Spec.Containers[i]
		container.Resources.Requests[v1.ResourceMemory] = resource.MustParse(spec.memory)
		container.Resources.Limits[v1.ResourceMemory] = resource.MustParse(spec.memory)
		container.Resources.Requests[v1.ResourceCPU] = resource.MustParse(spec.cpu)
		container.Resources.Limits[v1.ResourceCPU] = resource.MustParse(spec.cpu)
		for _, migConfig := range migConfigs {
			if spec.migSlice[migConfig] > 0 {
				container.Resources.Requests[v1.ResourceName(migConfig)] = resource.MustParse("1")
				container.Resources.Limits[v1.ResourceName(migConfig)] = resource.MustParse("1")
			} else {
				delete(container.Resources.Requests, v1.ResourceName(migConfig))
				delete(container.Resources.Limits, v1.ResourceName(migConfig))
			}
		}
	}
}

func (a *Autoscaler) waitForNewRevision(client serving.KnServingClient, oldService *kv1.Service) {
	log.Printf("Waiting for new revision to be ready...")
	for {
		changedService, err := client.GetService(context.TODO(), oldService.Name)
		if err != nil {
			log.Fatalf("Failed to get Knative service: %v", err)
			return
		}
		if changedService.Status.LatestReadyRevisionName != oldService.Status.LatestReadyRevisionName {
			break
		}
		time.Sleep(1 * time.Second)
	}
}

func (a *Autoscaler) changeTrafficToNewRevision(client serving.KnServingClient, newService *kv1.Service) {
	log.Printf("Change traffic to the new revision")
	newService.Status.Traffic[0].RevisionName = newService.Status.LatestReadyRevisionName
	_, err := client.UpdateService(context.Background(), newService)
	if err != nil {
		log.Fatalf("Failed to update service %s: %v", newService.Name, err)
	}
}

func (a *Autoscaler) deleteOldRevision(client serving.KnServingClient, oldService *kv1.Service) {
	log.Printf("Deleting the old revision")
	time.Sleep(30 * time.Second) // time for old revision to finish requests in progress
	err := client.DeleteRevision(context.Background(), oldService.Status.LatestReadyRevisionName, time.Hour*24)
	if err != nil {
		log.Fatalf("Failed to delete old revision %s: %v", oldService.Status.LatestReadyRevisionName, err)
	}
	log.Printf("Old revision deleted") // but the pods is still in terminate state
	EventsChan <- Event{
		revisionName: oldService.Status.LatestReadyRevisionName,
		scalingType:  SCALING_IN,
		resourceName: "",
	}
}

func (a *Autoscaler) labelServiceAsDone(client serving.KnServingClient, newService *kv1.Service) {
	newService.Labels["auto-scaler"] = "done"
	_, err := client.UpdateService(context.Background(), newService)
	if err != nil {
		log.Fatalf("Failed to update service %s: %v", newService.Name, err)
	}
}

func (a *Autoscaler) sendScalingEvent(service *kv1.Service, spec ResourceSpec) {
	for slice, count := range spec.migSlice {
		if count > 0 {
			EventsChan <- Event{
				revisionName: service.Status.LatestReadyRevisionName,
				scalingType:  spec.scalingType,
				resourceName: slice,
			}
			break
		}
	}
}

func (a *Autoscaler) processKnativeService(serviceName string) {
	podList, err := a.Clientset.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("serving.knative.dev/service=%s", serviceName),
	})
	if err != nil {
		log.Printf("Failed to list pods for service %s: %v", serviceName, err)
		return
	}

	podMetrics := make(map[string]float64)

	for _, pod := range podList.Items {
		podName := pod.Name

		// Check if pod is running
		if pod.Status.Phase != v1.PodRunning || pod.DeletionTimestamp != nil {
			log.Printf("Pod %s is not running", podName)
			EventsChan <- Event{
				revisionName: pod.Labels["serving.knative.dev/revision"],
				scalingType:  NOT_SCALING,
				resourceName: "",
			}
			continue
		}

		// Obtain SLO from pod's label
		SLO, err := strconv.ParseFloat(pod.Labels["slo"], 64)
		if err != nil {
			log.Printf("Failed to parse SLO for pod %s: %v", podName, err)
			continue
		}

		// Get node resources
		nodeName := pod.Spec.NodeName
		nodeResources, err := a.getNodeResources(nodeName)
		if err != nil {
			log.Printf("Error getting resources for pod %s: %v", podName, err)
			continue
		}

		// Obtain current MIG GPU used by pod
		usingMigGPU := false
		var gpuUsedIndex int64
		for index, migConfig := range migConfigs {
			rQuant := pod.Spec.Containers[0].Resources.Requests[v1.ResourceName(migConfig)]
			if rQuant.Value() > 0 {
				usingMigGPU = true
				gpuUsedIndex = int64(index)
				break // since currently a pod (CUDA process) can only use one MIG GPU
			}
		}
		if !usingMigGPU {
			log.Printf("Pod %s is not using any GPU", podName)
			continue
		}
		log.Printf("GPU used by pod %s: %s", podName, migConfigs[gpuUsedIndex])

		// Send Prometheus query
		queryList := map[string]string{
			TGI_REQUEST_METRIC: fmt.Sprintf(`increase(tgi_request_mean_time_per_token_duration_sum{pod="%s"}[1m])/increase(tgi_request_mean_time_per_token_duration_count{pod="%s"}[1m])`,
				podName, podName),
			TGI_REQUEST_METRIC_COUNT: fmt.Sprintf(`increase(tgi_request_mean_time_per_token_duration_count{pod="%s"}[1m])`, podName),
		}
		for name, query := range queryList {
			a.fetchPrometheusData(name, query, podMetrics)
		}

		// If not receiving request in 60s -> scale down directly
		notReceivingRequest := false
		value, exists := podMetrics[TGI_REQUEST_METRIC_COUNT]
		if !exists {
			// prometheus hasn't recognize the pod
			log.Printf("Promeheus hasn't found the pod in revision: %s", pod.Labels["serving.knative.dev/revision"])
			EventsChan <- Event{
				revisionName: pod.Labels["serving.knative.dev/revision"],
				scalingType:  NOT_SCALING,
				resourceName: migConfigs[gpuUsedIndex],
			}
			return
		}

		if exists && value == 0 {
			log.Printf("Pod %s is not receiving request", podName)
			notReceivingRequest = true
		}

		// Assign processed metrics to PodResources struct for further reference
		podData := PodData{
			podName:            podName,
			currentGPU:         gpuUsedIndex,
			TGI_REQUEST_METRIC: podMetrics[TGI_REQUEST_METRIC],
		}

		// Check if should scale
		scaleDecision, spec := a.shouldScale(podData, nodeResources, SLO, notReceivingRequest)
		if scaleDecision == SCALING_UP || scaleDecision == SCALING_DOWN {
			log.Println("Scaling decision:", scaleDecision, "Spec:", spec)

			a.labelScaling(serviceName)

			spec.slo = SLO
			go a.scaleKnativeService(serviceName, spec)
		} else {
			log.Printf("Pod %s is not scaling", podName)
			EventsChan <- Event{
				revisionName: pod.Labels["serving.knative.dev/revision"],
				scalingType:  NOT_SCALING,
				resourceName: migConfigs[podData.currentGPU],
			}
		}
	}
}

// labelScaling labels the service that is being scaling, preventing it from being processed again
func (a *Autoscaler) labelScaling(serviceName string) {
	client, err := a.createKnativeClient()
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	// Get the existing service instance
	oldService, err := client.GetService(context.TODO(), serviceName)
	if err != nil {
		log.Fatalf("Failed to get Knative service: %v", err)
		return
	}

	// create empty service instance
	// copy spec of unscaled service to new service
	newService := oldService.DeepCopy()
	if newService.Labels == nil {
		newService.Labels = make(map[string]string)
	}
	newService.Labels["auto-scaler"] = "scaling"

	//Create the new service with the updated spec
	ctx := context.Background()
	log.Printf("Update Knative service: %s's label", serviceName)
	_, err = client.UpdateService(ctx, newService)
	if err != nil {
		log.Printf("Error creating Knative service: %s", err.Error())
	}

	log.Printf("Service %s labeled for scaling", serviceName)
}

func (a *Autoscaler) createKnativeClient() (serving.KnServingClient, error) {
	p := commands.KnParams{}
	p.Initialize()
	return p.NewServingClient("default")
}

func main() {
	log.Printf("Starting Autoscaler...\n")
	Autoscaler, err := NewAutoscaler() // create new metric fetcher, initialize all member variables
	if err != nil {
		log.Fatalf("Failed to initialize metrics fetcher: %v", err)
	}

	p := commands.KnParams{}
	p.Initialize()
	// Create new knative serving client
	client, err := p.NewServingClient("default")
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	Exporter := NewExporter()
	go Exporter.StartExporter(Autoscaler.ScrapeInterval) // start the exporter to expose metrics

	for { // check within interval time
		services, err := client.ListServices(context.TODO())
		if err != nil {
			log.Fatalf("Failed to list Knative services in namespace %s: %v", "default", err)
		}
		for _, service := range services.Items { // iterate through all Knative services in namespace default
			serviceName := service.Name
			if !strings.Contains(serviceName, "autoscaler") && !strings.Contains(serviceName, "dispatcher") && service.Labels["auto-scaler"] != "scaling" {
				log.Printf("\n\nProcessing Knative service: %s\n", serviceName)
				Autoscaler.processKnativeService(serviceName)
			}
		}
		time.Sleep(Autoscaler.ScrapeInterval)
	}
}
