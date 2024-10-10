package main

import (
	"context"
	"log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Processor struct{}

type ServiceSpec struct {
	CPU        int
	GPU_slices map[string]int
	Memory     int
	Env        map[string]string
	Name       string
	Model      string
}

type ResourceEstimate struct {
	CPU        int
	GPU_slices map[string]int
	Memory     int
}

// DecideService fills the remaining information in the ServiceSpec based on the RequestGroup and ResourceEstimate
func (d Processor) DecideService(group RequestGroup) ServiceSpec {
	log.Println("Deciding service spec based on request group")
	resourceEstimate := d.ResourceEstimate(group)

	spec := ServiceSpec{
		CPU:        resourceEstimate.CPU,
		GPU_slices: resourceEstimate.GPU_slices,
		Memory:     resourceEstimate.Memory,
		Env:        group.Requests[0].Env,
		Name:       group.Requests[0].Model,
		Model:      group.Requests[0].Model,
	}
	//log.Printf("Decided ServiceSpec - CPU: %d, GPU: %d, Memory: %d, ServiceName: %s, Model: %s, SLO: %d", spec.CPU, spec.GPU, spec.Memory, spec.ServiceName, spec.Model, spec.SLO)
	return spec
}

// Estimate Resource usage for a RequestGroup
func (d Processor) ResourceEstimate(group RequestGroup) ResourceEstimate {
	// Policy , gives smallest slice availablem on cluster.
	log.Println("Estimating resources for request group")

	var totalCPU int
	var totalMemory int
	var migConfigMap = map[string]int{
		"nvidia.com/mig-1g.5gb":  0,
		"nvidia.com/mig-2g.10gb": 0,
		"nvidia.com/mig-3g.20gb": 0,
		"nvidia.com/mig-4g.20gb": 0,
		"nvidia.com/mig-7g.40gb": 0,
	}

	var migConfigList = []string{
		"nvidia.com/mig-1g.5gb",
		"nvidia.com/mig-2g.10gb",
		"nvidia.com/mig-3g.20gb",
		"nvidia.com/mig-4g.20gb",
		"nvidia.com/mig-7g.40gb",
	}
	//CPU, Memory logic define here
	totalCPU = 10000     // 10 CPUs, TGI requires massive ammount of cpu and memory , or else there will be error occured
	totalMemory = 102400 // 100GB , TGI requires massive ammount of cpu and memory , or else there will be error occured

	// GPU logic define here //
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to create in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create clientset: %v", err)
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Failed to list nodes: %v", err)
	}

	for _, node := range nodes.Items {
		for _, migConfig := range migConfigList {
			if Quantity, ok := node.Status.Capacity[v1.ResourceName(migConfig)]; ok {
				migConfigMap[migConfig] += int(Quantity.Value())
			}
		}
	}
	log.Printf("Available MIG slices: %v", migConfigMap)

	// Find the smallest available GPU slice
	smallestSlice := ""

	for _, migConfig := range migConfigList {
		if migConfigMap[migConfig] > 0 {
			smallestSlice = migConfig
			break
		}
	}
	log.Print("Assigned resources , CPU : ", totalCPU, " Memory : ", totalMemory, " GPU : ", smallestSlice)

	return ResourceEstimate{
		CPU:        totalCPU,                         // Total CPU estimate
		GPU_slices: map[string]int{smallestSlice: 1}, // Set to 0 for now, unless GPU resources are also required
		Memory:     totalMemory,                      // Total Memory estimate
	}
}
