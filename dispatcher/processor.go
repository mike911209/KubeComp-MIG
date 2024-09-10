package main

import (
	"log"
)

type Processor struct{}

type ServiceSpec struct {
	CPU         int
	GPU_slices  map[string]int
	Memory      int
	ServiceName string
	groupLabel  map[string]string
	Model       string
	SLO         int
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
		CPU:         resourceEstimate.CPU,
		GPU_slices:  resourceEstimate.GPU_slices,
		Memory:      resourceEstimate.Memory,
		ServiceName: group.Requests[0].Model,
		groupLabel:  map[string]string{"app": "example"},
		Model:       group.Requests[0].Model,
		SLO:         group.MinSLO,
	}

	//log.Printf("Decided ServiceSpec - CPU: %d, GPU: %d, Memory: %d, ServiceName: %s, Model: %s, SLO: %d",
	//	spec.CPU, spec.GPU, spec.Memory, spec.ServiceName, spec.Model, spec.SLO)

	return spec
}

// Estimate Resource usage for a RequestGroup
func (d Processor) ResourceEstimate(group RequestGroup) ResourceEstimate {
	log.Println("Estimating resources for request group")

	var totalCPU int
	var totalMemory int

	// Iterate over all requests in the group to sum up the resource usage based on token size
	for _, req := range group.Requests {
		// Scale CPU and Memory based on token size
		totalCPU += req.TokenSize*5 + 300    // Example: CPU estimate based on token size
		totalMemory += req.TokenSize*5 + 500 // Memory estimate based on token size
	}

	// GPU logic define here //

	//

	//log.Printf("Estimated Resources - CPU: %d, Memory: %d, GPU: %d", totalCPU, totalMemory, )

	return ResourceEstimate{
		CPU:        totalCPU,                                   // Total CPU estimate
		GPU_slices: map[string]int{"nvidia.com/mig-1g.5gb": 1}, // Set to 0 for now, unless GPU resources are also required
		Memory:     totalMemory,                                // Total Memory estimate
	}
}
