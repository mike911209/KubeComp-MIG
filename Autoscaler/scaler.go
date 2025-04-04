package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	serving "knative.dev/client/pkg/serving/v1"
	kv1 "knative.dev/serving/pkg/apis/serving/v1"
)

type ScaleDecision int

const (
	NotScaling ScaleDecision = iota
	ScalingUp
	ScalingDown
	ScalingOut
	ScalingIn
)

type Scaler interface {
	// TODO: change decide scale to interface, abstract the implementation (ScaleDecider)
	DecideScale(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements)
	checkScaleDown(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements)
	checkScaleUp(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements)
	checkScaleIn(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements)
	checkScaleOut(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements)
	ApplyScale(scaleDecision ScaleDecision, revisionData RevisionData, newResources v1.ResourceRequirements, khelper *KnativeHelper) error
	updateServiceTraffic(scaleDecision ScaleDecision, revisionData RevisionData, khelper *KnativeHelper) error
}

type SimpleScaler struct {
	DefaultCPU      string
	DefaultMemory   string
	gpuTierRegistry *GpuTierRegistry
}

func NewSimpleScaler(gpuTierRegistry *GpuTierRegistry) *SimpleScaler {
	// TODO: change cpu and memory also to config map
	defaultCPU := os.Getenv("DEFAULT_CPU")
	if defaultCPU == "" {
		defaultCPU = "1"
	}

	defaultMemory := os.Getenv("DEFAULT_MEMORY")
	if defaultMemory == "" {
		defaultMemory = "10Gi"
	}

	return &SimpleScaler{
		DefaultCPU:      defaultCPU,
		DefaultMemory:   defaultMemory,
		gpuTierRegistry: gpuTierRegistry,
	}
}

// Decide the Scaling decision based on the metrics
func (s *SimpleScaler) DecideScale(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements) {
	// TODO: generalize metric checking, not just hardcode TGI_REQUEST_METRIC (maybe multiple metrics)

	log.Printf("Checking scaling decision - Pod: %s, Metric: %v", revisionData.podName, revisionData.metrics)

	// TODO: implement scaling logic (scale up, scale down, scale out, scale in)
	// TODO: currently we only check one metric
	scaleDecision := NotScaling
	for metric, value := range revisionData.metrics {
		switch {
		case math.IsNaN(value) || value < metric.SLO*metric.ScaleDownFactor:
			scaleDecision = ScalingDown
		case value > metric.SLO*metric.ScaleUpFactor:
			scaleDecision = ScalingUp
		}
	}

	switch scaleDecision {
	case ScalingDown:
		return s.checkScaleDown(revisionData)
	case ScalingUp:
		return s.checkScaleUp(revisionData)
	default:
		log.Println("No scaling needed")
		return NotScaling, v1.ResourceRequirements{}
	}
}

// check if scaling decsion can be applied and generate resource requirements
func (s *SimpleScaler) checkScaleDown(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements) {
	// TODO: support MPS
	prevTier, err := s.gpuTierRegistry.GetPrevAvailTier(revisionData.gpuResource)
	if err != nil {
		log.Printf("Error getting previous available tier for pod %s: %v", revisionData.name, err)
		return NotScaling, v1.ResourceRequirements{}
	}

	resourceRequirements := v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:                    resource.MustParse(s.DefaultCPU),
			v1.ResourceMemory:                 resource.MustParse(s.DefaultMemory),
			v1.ResourceName(prevTier.gpuName): resource.MustParse("1"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:                    resource.MustParse(s.DefaultCPU),
			v1.ResourceMemory:                 resource.MustParse(s.DefaultMemory),
			v1.ResourceName(prevTier.gpuName): resource.MustParse("1"),
		},
	}

	log.Printf("Scaling down pod %s to %s", revisionData.name, prevTier.gpuName)

	return ScalingDown, resourceRequirements
}

// check if scaling decsion can be applied and generate resource requirements
func (s *SimpleScaler) checkScaleUp(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements) {
	nextTier, err := s.gpuTierRegistry.GetNextAvailTier(revisionData.gpuResource)
	if err != nil {
		log.Printf("Error getting next available tier for pod %s: %v", revisionData.name, err)
		return NotScaling, v1.ResourceRequirements{}
	}

	resourceRequirements := v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:                    resource.MustParse(s.DefaultCPU),
			v1.ResourceMemory:                 resource.MustParse(s.DefaultMemory),
			v1.ResourceName(nextTier.gpuName): resource.MustParse("1"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:                    resource.MustParse(s.DefaultCPU),
			v1.ResourceMemory:                 resource.MustParse(s.DefaultMemory),
			v1.ResourceName(nextTier.gpuName): resource.MustParse("1"),
		},
	}

	log.Printf("Scaling up pod %s to %s", revisionData.name, nextTier.gpuName)

	return ScalingUp, resourceRequirements
}

func (s *SimpleScaler) checkScaleIn(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements) {
	return ScalingIn, v1.ResourceRequirements{}
}

func (s *SimpleScaler) checkScaleOut(revisionData RevisionData) (ScaleDecision, v1.ResourceRequirements) {
	// TODO: not only scale to same resource, maybe consider using bigger or smaller
	sameTier, err := s.gpuTierRegistry.GetSameAvailTier(revisionData.gpuResource)
	if err != nil {
		log.Printf("Error getting same available tier for pod %s: %v", revisionData.name, err)
		return NotScaling, v1.ResourceRequirements{}
	}

	resourceRequirements := v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:                    resource.MustParse(s.DefaultCPU),
			v1.ResourceMemory:                 resource.MustParse(s.DefaultMemory),
			v1.ResourceName(sameTier.gpuName): resource.MustParse("1"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:                    resource.MustParse(s.DefaultCPU),
			v1.ResourceMemory:                 resource.MustParse(s.DefaultMemory),
			v1.ResourceName(sameTier.gpuName): resource.MustParse("1"),
		},
	}

	log.Printf("Scaling out pod %s to %s", revisionData.name, sameTier.gpuName)

	return ScalingOut, resourceRequirements
}

func incrementRevision(revision string) (string, error) {
	// Split the string by hyphen
	parts := strings.Split(revision, "-")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid revision format: %s", revision)
	}

	// Extract prefix and numeric part
	prefix := parts[0] // e.g., "test"
	numStr := parts[1] // e.g., "0001"

	// Convert numeric part to integer
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse number: %v", err)
	}

	// Increment the number
	num++

	// Format the new number with zero-padding (assuming 4 digits like "0001")
	newNumStr := fmt.Sprintf("%04d", num)

	// Reconstruct the string
	newRevision := fmt.Sprintf("%s-%s", prefix, newNumStr)
	return newRevision, nil
}

func (s *SimpleScaler) ApplyScale(scaleDecision ScaleDecision, revisionData RevisionData, newResources v1.ResourceRequirements, khelper *KnativeHelper) error {
	log.Printf("Applying scaling decision for revision %s", revisionData.name)

	if scaleDecision != ScalingIn {
		// step 1: create new revision based on updated resources
		oldService, err := khelper.GetService(context.TODO(), revisionData.svcName)
		if err != nil {
			return fmt.Errorf("error getting service %s: %v", revisionData.svcName, err)
		}

		newService := oldService.DeepCopy()
		newService.Spec.Template.Spec.PodSpec.Containers[0].Resources = newResources // TODO: maybe the first container's spec is not what we want to change
		if newService.Spec.Template.Labels == nil {
			newService.Spec.Template.Labels = make(map[string]string)
		}
		newService.Annotations["update-at"] = time.Now().Format(time.RFC3339)
		newService.Spec.Template.ObjectMeta.CreationTimestamp = metav1.Time{Time: time.Now()}

		_, err = khelper.UpdateService(context.TODO(), newService)
		if err != nil {
			return fmt.Errorf("error updating service %s: %v", newService.Name, err)
		}

		err = khelper.WaitService(context.TODO(), newService, 600*time.Second) // TODO: timeout should be configurable
		if err != nil {
			return fmt.Errorf("error waiting for service %s: %v", newService.Name, err)
		}
	}

	// step 2: update service's traffic
	err := s.updateServiceTraffic(scaleDecision, revisionData, khelper)
	if err != nil {
		return fmt.Errorf("error updating service traffic: %v", err)
	}

	// step 3: post scaling actions
	switch scaleDecision {
	case ScalingUp, ScalingDown, ScalingIn:
		khelper.DeleteRevision(context.TODO(), revisionData.name, 5*time.Minute)
	case ScalingOut:
	default:
		log.Printf("Unknown scaling decision: %d", scaleDecision)
	}

	log.Printf("Successfully scaled revision %s", revisionData.name)
	return nil
}

func (s *SimpleScaler) updateServiceTraffic(scaleDecision ScaleDecision, revisionData RevisionData, khelper *KnativeHelper) error {
	// TODO: update traffic percent based on resource

	// step 3: update service's traffic
	newService, err := khelper.GetService(context.TODO(), revisionData.svcName)
	if err != nil {
		return fmt.Errorf("error getting service %s: %v", revisionData.svcName, err)
	}

	revisionList, err := khelper.ListRevisions(context.TODO(), serving.WithLabel("serving.knative.dev/service", revisionData.svcName))
	if err != nil {
		return fmt.Errorf("error listing revisions: %v", err)
	}

	// TODO: fix traffic length != revision length

	newTraffic := newService.Spec.Traffic
	var basePercent, remainder int
	if scaleDecision == ScalingOut {
		basePercent = 100 / len(revisionList.Items)
		remainder = 100 % len(revisionList.Items)
	} else {
		basePercent = 100 / (len(revisionList.Items) - 1)
		remainder = 100 % (len(revisionList.Items) - 1)
	}
	switch scaleDecision {
	case ScalingIn:
		if len(revisionList.Items) == 1 {
			// TODO: scale to zero, maybe delete the knative service
			return fmt.Errorf("cannot scale in further")
		}
		var revisionIdx int
		for i, revision := range revisionList.Items {
			if revision.Name == revisionData.name {
				revisionIdx = i
				continue
			}
			percent := int64(basePercent)
			if i < remainder {
				percent++
			}
			newTraffic[i] = kv1.TrafficTarget{
				Percent:      &percent,
				RevisionName: revision.Name,
			}
		}
		newTraffic = append(newTraffic[:revisionIdx], newTraffic[revisionIdx+1:]...)
	case ScalingDown, ScalingUp:
		var revisionIdx int
		for i, revision := range revisionList.Items {
			if revision.Name == revisionData.name {
				revisionIdx = i
				continue
			}
			percent := int64(basePercent)
			if i < remainder {
				percent++
			}
			if i == len(revisionList.Items)-1 {
				newTraffic = append(newTraffic, kv1.TrafficTarget{
					Percent:      &percent,
					RevisionName: newService.Status.LatestReadyRevisionName,
				})
			} else {
				newTraffic[i] = kv1.TrafficTarget{
					Percent:      &percent,
					RevisionName: revision.Name,
				}
			}
		}
		newTraffic = append(newTraffic[:revisionIdx], newTraffic[revisionIdx+1:]...)
	case ScalingOut:
		for i, revision := range revisionList.Items {
			percent := int64(basePercent)
			if i < remainder {
				percent++
			}
			if i == len(revisionList.Items)-1 {
				newTraffic = append(newTraffic, kv1.TrafficTarget{
					Percent:      &percent,
					RevisionName: revision.Name,
				})
			} else {
				newTraffic[i] = kv1.TrafficTarget{
					Percent:      &percent,
					RevisionName: revision.Name,
				}
			}
		}
	}

	newService.Spec.Traffic = newTraffic
	_, err = khelper.UpdateService(context.TODO(), newService)
	if err != nil {
		return fmt.Errorf("error updating service %s: %v", newService.Name, err)
	}

	return nil
}
