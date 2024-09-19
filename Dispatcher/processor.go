package main

import (
	"log"
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
	log.Println("Estimating resources for request group")

	var totalCPU int
	var totalMemory int

	//CPU, Memory logic define here

	// Iterate over all requests in the group to sum up the resource usage based on token size
	// for _, req := range group.Requests {
	// 	// Scale CPU and Memory based on token size
	// 	totalCPU = 10000     // Example: CPU estimate based on token size
	// 	totalMemory = 102400 // Memory estimate based on token size
	// }

	totalCPU = 10000     // TGI requires massive ammount of cpu and memory , or else there will be error occured
	totalMemory = 102400 // TGI requires massive ammount of cpu and memory , or else there will be error occured

	// GPU logic define here //

	//log.Printf("Estimated Resources - CPU: %d, Memory: %d, GPU: %d", totalCPU, totalMemory, )

	return ResourceEstimate{
		CPU:        totalCPU,                            // Total CPU estimate
		GPU_slices: map[string]int{"nvidia.com/gpu": 1}, // Set to 0 for now, unless GPU resources are also required
		Memory:     totalMemory,                         // Total Memory estimate
	}
}
