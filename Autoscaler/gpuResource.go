package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

type GpuType int

const (
	MPS GpuType = iota
	MIG
	Normal
)

type GpuResource struct {
	gpuType GpuType
	gpuName string
	cpuSize float64
	memSize float64
}

func parseGpuResource(gpuName string) (GpuResource, error) {
	// Match MIG: nvidia.com/mig-Xg.Xgb
	migRe := regexp.MustCompile(`mig-(\d+)g\.(\d+)gb`)
	if migMatches := migRe.FindStringSubmatch(gpuName); len(migMatches) == 3 {
		cpu, _ := strconv.Atoi(migMatches[1])
		mem, _ := strconv.Atoi(migMatches[2])
		return GpuResource{
			gpuType: MIG,
			gpuName: gpuName,
			cpuSize: float64(cpu),
			memSize: float64(mem),
		}, nil
	}

	// Match MPS: nvidia.com/gpu-Xgb
	mpsRe := regexp.MustCompile(`gpu-(\d+)gb`)
	if mpsMatches := mpsRe.FindStringSubmatch(gpuName); len(mpsMatches) == 2 {
		mem, _ := strconv.Atoi(mpsMatches[1])
		return GpuResource{
			gpuType: MPS,
			gpuName: gpuName,
			cpuSize: float64(mem),
			memSize: float64(mem),
		}, nil
	}

	// Match Normal GPU: nvidia.com/gpu
	if gpuName == "nvidia.com/gpu" {
		return GpuResource{
			gpuType: Normal,
			gpuName: gpuName,
			cpuSize: math.NaN(),
			memSize: math.NaN(),
		}, nil
	}

	return GpuResource{}, fmt.Errorf("unsupported GPU name format: %s", gpuName)
}
