package main

import (
	"context"
	"fmt"
	"math"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type GpuTierRegistry struct {
	tiers      map[GpuType][]GpuResource
	availTiers map[GpuType]map[GpuResource]int
	kubeClient *kubernetes.Clientset
}

func (gtr *GpuTierRegistry) GetAllTiers(gpuType GpuType) []GpuResource {
	return gtr.tiers[gpuType]
}

func (gtr *GpuTierRegistry) UpdateAvailTiers() {
	gtr.availTiers = make(map[GpuType]map[GpuResource]int)

	nodeList, _ := gtr.kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	for node := range nodeList.Items {
		for rName, rQuant := range nodeList.Items[node].Status.Allocatable {
			gpuResource, err := parseGpuResource(rName.String())
			if err != nil {
				continue
			}
			if gtr.availTiers[gpuResource.gpuType] == nil {
				gtr.availTiers[gpuResource.gpuType] = make(map[GpuResource]int)
			}
			gtr.availTiers[gpuResource.gpuType][gpuResource] = int(rQuant.Value())
		}
	}
}

func (gtr *GpuTierRegistry) GetSameAvailTier(current GpuResource) (GpuResource, error) {
	gtr.UpdateAvailTiers()

	if gtr.availTiers[current.gpuType][current] > 0 {
		return current, nil
	}

	return GpuResource{}, fmt.Errorf("no same available tier found for %s", current.gpuName)
}

func (gtr *GpuTierRegistry) GetPrevAvailTier(current GpuResource) (GpuResource, error) {
	gtr.UpdateAvailTiers()

	list := gtr.tiers[current.gpuType]
	for idx, tier := range list {
		if tier.gpuName == current.gpuName {
			// Check until find the previous available tier
			for i := idx - 1; i >= 0; i-- {
				if gtr.availTiers[current.gpuType][list[i]] > 0 {
					return list[i], nil
				}
			}
		}
	}
	return GpuResource{}, fmt.Errorf("no prev available tier found for %s", current.gpuName)
}

func (gtr *GpuTierRegistry) GetNextAvailTier(current GpuResource) (GpuResource, error) {
	gtr.UpdateAvailTiers()

	list := gtr.tiers[current.gpuType]
	for idx, tier := range list {
		if tier.gpuName == current.gpuName {
			// Check until find the next available tier
			for i := idx + 1; i < len(list); i++ {
				if gtr.availTiers[current.gpuType][list[i]] > 0 {
					return list[i], nil
				}
			}
		}
	}
	return GpuResource{}, fmt.Errorf("no next available tier found for %s", current.gpuName)
}

func NewGpuTierRegistry(kubeClient *kubernetes.Clientset) *GpuTierRegistry {
	gtr := &GpuTierRegistry{
		tiers:      make(map[GpuType][]GpuResource),
		availTiers: nil,
		kubeClient: kubeClient,
	}

	// Initialize the tiers
	// TODO: put these gpuName into configmap, parse the name by parseGpuResource function
	gtr.tiers[MIG] = []GpuResource{
		{gpuType: MIG, gpuName: "nvidia.com/mig-1g.5gb", cpuSize: 1, memSize: 5},
		{gpuType: MIG, gpuName: "nvidia.com/mig-2g.10gb", cpuSize: 2, memSize: 10},
		{gpuType: MIG, gpuName: "nvidia.com/mig-3g.20gb", cpuSize: 3, memSize: 20},
		{gpuType: MIG, gpuName: "nvidia.com/mig-4g.20gb", cpuSize: 4, memSize: 20},
		{gpuType: MIG, gpuName: "nvidia.com/mig-7g.40gb", cpuSize: 7, memSize: 40},
	}
	gtr.tiers[MPS] = []GpuResource{
		{gpuType: MPS, gpuName: "nvidia.com/gpu-1gb", cpuSize: 1, memSize: 1},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-2gb", cpuSize: 2, memSize: 2},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-3gb", cpuSize: 3, memSize: 3},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-4gb", cpuSize: 4, memSize: 4},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-5gb", cpuSize: 5, memSize: 5},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-6gb", cpuSize: 6, memSize: 6},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-7gb", cpuSize: 7, memSize: 7},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-8gb", cpuSize: 8, memSize: 8},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-9gb", cpuSize: 9, memSize: 9},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-10gb", cpuSize: 10, memSize: 10},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-11gb", cpuSize: 11, memSize: 11},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-12gb", cpuSize: 12, memSize: 12},
		{gpuType: MPS, gpuName: "nvidia.com/gpu-13gb", cpuSize: 13, memSize: 13},
	}
	gtr.tiers[Normal] = []GpuResource{
		{gpuType: Normal, gpuName: "nvidia.com/gpu", cpuSize: math.NaN(), memSize: math.NaN()},
	}

	// Update the available tiers
	gtr.UpdateAvailTiers()

	return gtr
}
