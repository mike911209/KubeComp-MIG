package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
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
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
)

const prometheusURL = "http://prometheus-kube-prometheus-prometheus:9090/api/v1/query"

// Globally defined MIG configurations, from small to big
var migConfigs = []string{
	"nvidia.com/mig-1g.5gb",
	"nvidia.com/mig-2g.10gb",
	"nvidia.com/mig-3g.20gb",
	"nvidia.com/mig-4g.20gb",
	"nvidia.com/mig-7g.40gb",
}

type PodResources struct {
	CPUUsage    float64
	MemoryUsage float64
}
type NodeResources struct {
	NodeName    string
	TotalMemory float64
}
type ResourceSpec struct {
	cpu      string
	memory   string
	migSlice map[string]int
}

type Autoscaler struct {
	Clientset      *kubernetes.Clientset
	ScrapeInterval time.Duration
}

func NewAutoscaler() (*Autoscaler, error) {
	scrapeInterval := os.Getenv("METRICS_SCRAPE_INTERVAL")
	if scrapeInterval == "" {
		scrapeInterval = "120"
	}

	interval, err := time.ParseDuration(scrapeInterval + "s")
	if err != nil {
		return nil, fmt.Errorf("Invalid scrape interval: %v", err)
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("Failed to create in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("Failed to create Kubernetes client: %v", err)
	}

	return &Autoscaler{
		Clientset:      clientset,
		ScrapeInterval: interval,
	}, nil
}

func (a *Autoscaler) fetchPrometheusData(metric string, query string, serviceMetrics map[string]map[string]float64) error {

	// TODO: connect to Prometheus to fetch metrics
	resp, err := http.Get(fmt.Sprintf("%s?query=%s", prometheusURL, query))
	if err != nil {
		return fmt.Errorf("Failed to fetch data from Prometheus: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to read response body: %v", err)
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
		return fmt.Errorf("Failed to unmarshal JSON: %v", err)
	}

	for _, res := range result.Data.Result {
		value, err := strconv.ParseFloat(res.Value[1].(string), 64)
		if err != nil {
			return fmt.Errorf("Failed to parse value: %v", err)
		}
		podName := res.Metric["pod"]
		if _, exists := serviceMetrics[podName]; !exists {
			serviceMetrics[podName] = make(map[string]float64)
		}
		serviceMetrics[podName][metric] = value
	}

	return nil
}

func (a *Autoscaler) getNodeResources(nodeName string) (NodeResources, error) {
	// TODO: add more node infos if needed
	node, err := a.Clientset.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Failed to get node: %v", err)
		return NodeResources{}, nil
	}

	totalMemory := node.Status.Capacity.Memory().Value()
	log.Println("Node resources - NodeName:", nodeName, "TotalMemory:", totalMemory)
	return NodeResources{
		NodeName:    nodeName,
		TotalMemory: float64(totalMemory),
	}, nil
}

func (a *Autoscaler) shouldScale(pR PodResources, nR NodeResources) bool {
	//TODO : define scaling decision logic
	log.Println("Checking if should scale - PodResources:", pR, "NodeResources:", nR)
	return true
}

func (a *Autoscaler) recreateKnativeService(serviceName string, spec ResourceSpec) {
	// TODO : deal with boundary cases:
	// 1. If the service has same spec --> dont scale
	// 2. Check if service exists before scaling
	// 3. Check if service state is ready
	// 4. Create thread to process scaling for a service

	log.Printf("Recreating Knative service - Name: %s", serviceName)

	// Create new knative serving client
	p := commands.KnParams{}
	p.Initialize()
	client, err := p.NewServingClient("default")
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	// Get the existing service instance
	service, err := client.GetService(context.TODO(), serviceName)
	if err != nil {
		log.Fatalf("Failed to get Knative service: %v", err)
		return
	}

	// create empty service instance
	// Note , you can't just deepcopy the whole service, since there are some old metadata that cause errors
	var newService = &servingv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: "default",
		},
	}
	// copy spec of unscaled service to new service
	newService.Spec = service.Spec

	log.Println("Waiting for original service to be deleted...")
	// Delete the unscaled service
	err = client.DeleteService(context.TODO(), serviceName, time.Hour*24) // Wait until service delete , timeout 1 day
	if err != nil {
		log.Println("Error deleting Knative service", err.Error(), "*Scale process aborted*")
		return
	}
	log.Println("Original service deleted.")

	// Modify the copy with the new resource settings
	for i := range newService.Spec.Template.Spec.Containers {
		newService.Spec.Template.Spec.Containers[i].Resources.Requests[v1.ResourceMemory] = resource.MustParse(spec.memory)
		newService.Spec.Template.Spec.Containers[i].Resources.Limits[v1.ResourceMemory] = resource.MustParse(spec.memory)
		newService.Spec.Template.Spec.Containers[i].Resources.Requests[v1.ResourceCPU] = resource.MustParse(spec.cpu)
		newService.Spec.Template.Spec.Containers[i].Resources.Limits[v1.ResourceCPU] = resource.MustParse(spec.cpu)
	}
	log.Println("New service spec:", newService.Spec)

	//Create the new service with the updated spec
	ctx := context.Background()
	err = client.CreateService(ctx, newService)
	if err != nil {
		log.Println("Error creating Knative service: %s", err.Error())
	}

	log.Println("Knative service recreated(scaled) successfully")
}

func (a *Autoscaler) processKnativeService(serviceName string) {
	//define own query string
	// serviceMetrics := make(map[string]map[string]float64)
	// queryList := map[string]string{
	// 	"memory": fmt.Sprintf(`sum(container_memory_usage_bytes{namespace="%s",pod=~"%s-.*"})by(pod)`, namespace, serviceName),
	// }
	// for name, query := range queryList {
	// 	a.fetchPrometheusData(name, query, serviceMetrics)
	// }

	// Mock data , remove when Prometheus is connected
	pods, _ := a.Clientset.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})
	serviceMetrics := map[string]map[string]float64{
		pods.Items[0].Name: {
			"memory": 100,
			"cpu":    1,
		},
	}

	log.Println("Service metrics:", serviceMetrics)

	for podName, podMetrics := range serviceMetrics { // in our experiment , only one pod in each service, for loop will run once here
		podInfo, err := a.Clientset.CoreV1().Pods("default").Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			log.Printf("Failed to get pod info %s: %v", podName, err)
			continue
		}

		// fetch node resources
		nodeName := podInfo.Spec.NodeName
		NodeResources, err := a.getNodeResources(nodeName)
		if err != nil {
			log.Printf("Error getting resources for pod %s: %v", podName, err)
			continue
		}

		// assign processed metrics to PodResources struct for further reference
		podResources := PodResources{
			MemoryUsage: podMetrics["memory"],
			CPUUsage:    podMetrics["cpu"],
		}
		log.Println("Pod resources:", podResources.MemoryUsage, podResources.CPUUsage)

		//check if should scale
		if a.shouldScale(podResources, NodeResources) {

			// TODO : define your own logic to decide the new resource spec
			spec := ResourceSpec{
				cpu:    "1",
				memory: "90Gi",
			}

			//scale service by spec
			a.recreateKnativeService(serviceName, spec)
		}
	}
}

func main() {
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

	services, err := client.ListServices(context.TODO())
	if err != nil {
		log.Fatalf("Failed to list Knative services in namespace %s: %v", "default", err)
	}

	for { // check within interval time
		for _, service := range services.Items { // iterate through all Knative services in namespace default
			serviceName := service.Name
			if !strings.Contains(serviceName, "autoscaler") && !strings.Contains(serviceName, "dispatcher") {
				log.Printf("Processing Knative service: %s\n", serviceName)
				Autoscaler.processKnativeService(serviceName)
			}
		}
		time.Sleep(Autoscaler.ScrapeInterval)
	}
}
