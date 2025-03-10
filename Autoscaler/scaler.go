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
	DecideScale(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements)
	checkScaleDown(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements)
	checkScaleUp(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements)
	checkScaleIn(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements)
	checkScaleOut(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements)
	ApplyScale(scaleDecision ScaleDecision, revisionData RevisionData, newResources v1.ResourceRequirements, khelper *KnativeHelper) error
	updateServiceTraffic(scaleDecision ScaleDecision, revisionData RevisionData, khelper *KnativeHelper) error
}

type SimpleScaler struct {
	ScaleUpThreshold   float64
	ScaleDownThreshold float64
	DefaultCPU         string
	DefaultMemory      string
}

func NewSimpleScaler() *SimpleScaler {
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

	defaultCPU := os.Getenv("DEFAULT_CPU")
	if defaultCPU == "" {
		defaultCPU = "1"
	}

	defaultMemory := os.Getenv("DEFAULT_MEMORY")
	if defaultMemory == "" {
		defaultMemory = "10Gi"
	}

	return &SimpleScaler{
		ScaleUpThreshold:   scaleUpThreshold,
		ScaleDownThreshold: scaleDownThreshold,
		DefaultCPU:         defaultCPU,
		DefaultMemory:      defaultMemory,
	}
}

func (s *SimpleScaler) DecideScale(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements) {
	// TODO: generalize metric checking, not just hardcode TGI_REQUEST_METRIC (maybe multiple metrics)

	log.Printf("Checking scaling decision - Pod: %s, Metric: %f",
		revisionData.name, revisionData.metrics[TGI_REQUEST_METRIC])

	notReceivingRequest := revisionData.metrics[TGI_REQUEST_METRIC] == 0

	// TODO: implement scaling logic (scale up, scale down, scale out, scale in)
	switch {
	case notReceivingRequest || revisionData.metrics[TGI_REQUEST_METRIC] < revisionData.slo*s.ScaleDownThreshold:
		return s.checkScaleDown(revisionData, nodeResources)
	case revisionData.metrics[TGI_REQUEST_METRIC] > revisionData.slo*s.ScaleUpThreshold:
		return s.checkScaleUp(revisionData, nodeResources)
	default:
		log.Println("No scaling needed")
		return NotScaling, v1.ResourceRequirements{}
	}
}

func (s *SimpleScaler) checkScaleDown(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements) {
	// TODO: support MPS
	if revisionData.gpuName == migConfigs[0] {
		log.Printf("Pod %s is already using the smallest resource tier", revisionData.name)
		return NotScaling, v1.ResourceRequirements{}
	}

	// TODO: Scale down to the next smaller resource tier (maybe using for loop)
	nextTier := migConfigs[migConfigsIdx[revisionData.gpuName]-1]
	if available := nodeResources.migSlices[nextTier]; available > 0 {
		resourceRequirements := v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceCPU:            resource.MustParse(s.DefaultCPU),
				v1.ResourceMemory:         resource.MustParse(s.DefaultMemory),
				v1.ResourceName(nextTier): resource.MustParse("1"),
			},
			Requests: v1.ResourceList{
				v1.ResourceCPU:            resource.MustParse(s.DefaultCPU),
				v1.ResourceMemory:         resource.MustParse(s.DefaultMemory),
				v1.ResourceName(nextTier): resource.MustParse("1"),
			},
		}
		log.Printf("Scaling down pod %s to %s", revisionData.name, nextTier)
		return ScalingDown, resourceRequirements
	}

	log.Printf("No available resources for scaling down pod %s to %s", revisionData.name, nextTier)
	return NotScaling, v1.ResourceRequirements{}
}

func (s *SimpleScaler) checkScaleUp(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements) {
	if revisionData.gpuName == migConfigs[len(migConfigs)-1] {
		log.Printf("Pod %s is already using the largest resource tier", revisionData.name)
		return NotScaling, v1.ResourceRequirements{}
	}

	nextTier := migConfigs[migConfigsIdx[revisionData.gpuName]+1]
	if available := nodeResources.migSlices[nextTier]; available > 0 {
		resourceRequirements := v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceCPU:            resource.MustParse(s.DefaultCPU),
				v1.ResourceMemory:         resource.MustParse(s.DefaultMemory),
				v1.ResourceName(nextTier): resource.MustParse("1"),
			},
			Requests: v1.ResourceList{
				v1.ResourceCPU:            resource.MustParse(s.DefaultCPU),
				v1.ResourceMemory:         resource.MustParse(s.DefaultMemory),
				v1.ResourceName(nextTier): resource.MustParse("1"),
			},
		}
		log.Printf("Scaling up pod %s to %s", revisionData.name, nextTier)
		return ScalingUp, resourceRequirements
	}

	log.Printf("No available resources for scaling up pod %s to %s", revisionData.name, nextTier)
	return NotScaling, v1.ResourceRequirements{}
}

func (s *SimpleScaler) checkScaleIn(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements) {
	return ScalingIn, v1.ResourceRequirements{}
}

func (s *SimpleScaler) checkScaleOut(revisionData RevisionData, nodeResources NodeResources) (ScaleDecision, v1.ResourceRequirements) {
	// TODO: not only scale to same resource, maybe consider using bigger or smaller
	if available := nodeResources.migSlices[revisionData.gpuName]; available > 0 {
		resourceRequirements := v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceCPU:                        resource.MustParse(s.DefaultCPU),
				v1.ResourceMemory:                     resource.MustParse(s.DefaultMemory),
				v1.ResourceName(revisionData.gpuName): resource.MustParse("1"),
			},
			Requests: v1.ResourceList{
				v1.ResourceCPU:                        resource.MustParse(s.DefaultCPU),
				v1.ResourceMemory:                     resource.MustParse(s.DefaultMemory),
				v1.ResourceName(revisionData.gpuName): resource.MustParse("1"),
			},
		}
		log.Printf("Scaling out pod %s to %s", revisionData.name, revisionData.gpuName)
		return ScalingOut, resourceRequirements
	}

	log.Printf("No available resources for scaling out pod %s to %s", revisionData.name, revisionData.gpuName)
	return NotScaling, v1.ResourceRequirements{}
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
			return fmt.Errorf("Error getting service %s: %v", revisionData.svcName, err)
		}

		newService := oldService.DeepCopy()
		newService.Spec.Template.Spec.PodSpec.Containers[0].Resources = newResources // TODO: maybe the first container's spec is not what we want to change
		if newService.Spec.Template.Labels == nil {
			newService.Spec.Template.Labels = make(map[string]string)
		}
		newService.Spec.Template.Labels["slo"] = fmt.Sprintf("%f", revisionData.slo)
		if newService.Annotations == nil {
			newService.Annotations = make(map[string]string)
		}
		newService.Annotations["update-at"] = time.Now().Format(time.RFC3339)
		newService.Spec.Template.ObjectMeta.CreationTimestamp = metav1.Time{Time: time.Now()}

		_, err = khelper.UpdateService(context.TODO(), newService)
		if err != nil {
			return fmt.Errorf("Error updating service %s: %v", newService.Name, err)
		}

		err = khelper.WaitService(context.TODO(), newService, 30*time.Second) // TODO: timeout should be configurable
		if err != nil {
			return fmt.Errorf("Error waiting for service %s: %v", newService.Name, err)
		}
	}

	// step 2: update service's traffic
	err := s.updateServiceTraffic(scaleDecision, revisionData, khelper)
	if err != nil {
		return fmt.Errorf("Error updating service traffic: %v", err)
	}

	// step 4: post scaling actions
	switch scaleDecision {
	case ScalingUp, ScalingDown, ScalingIn:
		khelper.DeleteRevision(context.TODO(), revisionData.name, 5*time.Minute)
	case ScalingOut:
	default:
		log.Printf("Unknown scaling decision: %d", scaleDecision)
	}

	log.Printf("Successfully scaled revision %s to %s", revisionData.name, newResources)
	return nil
}

func (s *SimpleScaler) updateServiceTraffic(scaleDecision ScaleDecision, revisionData RevisionData, khelper *KnativeHelper) error {
	// TODO: update traffic percent based on resource

	// step 3: update service's traffic
	newService, err := khelper.GetService(context.TODO(), revisionData.svcName)
	if err != nil {
		return fmt.Errorf("Error getting service %s: %v", revisionData.svcName, err)
	}

	revisionList, err := khelper.ListRevisions(context.TODO(), serving.WithLabel("serving.knative.dev/service", revisionData.svcName))
	if err != nil {
		return fmt.Errorf("Error listing revisions: %v", err)
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
			return fmt.Errorf("Cannot scale in further")
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
		return fmt.Errorf("Error updating service %s: %v", newService.Name, err)
	}

	return nil
}
