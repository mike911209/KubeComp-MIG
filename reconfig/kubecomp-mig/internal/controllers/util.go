package controllers

import (
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	targetPodLabel       string = "targetPod"
	targetNamespaceLabel string = "targetNamespace"
	kubecompStatus       string = "kubecomp.com/reconfig.state"
	nvConfigLabel        string = "nvidia.com/mig.config"
	nvMigStateLabel      string = "nvidia.com/mig.config.state"
	preprocessLabel      string = "preprocess"
	migConfigPath        string = "/etc/config/config.yaml"
	migResources         string = "nvidia.com/mig"
	podTemplateHash      string = "pod-template-hash"
	gpuIDLabel           string = "gpuIDs"
)

type MigConfig struct {
	Devices    []int          `yaml:"devices"`
	MigEnabled bool           `yaml:"mig-enabled"`
	MigDevices map[string]int `yaml:"mig-devices"`
}

type MigConfigYaml struct {
	Version    string                 `yaml:"version"`
	MigConfigs map[string][]MigConfig `yaml:"mig-configs"`
}

type Pod struct {
	name      string
	namespace string
}

type Status string

type Device struct {
	// ResourceName is the name of the resource exposed to k8s
	// (e.g. nvidia.com/gpu, nvidia.com/mig-2g10gb, etc.)
	ResourceName corev1.ResourceName
	// DeviceId is the actual ID of the underlying device
	// (e.g. ID of the GPU, ID of the MIG device, etc.)
	DeviceId string
	// Status represents the status of the k8s resource (e.g. free or used)
	Status Status
}

type KeyToQueue struct {
	lookup map[string][]string
}

func (k *KeyToQueue) IsEmpty() bool {
	return len(k.lookup) == 0
}

func (k *KeyToQueue) Add(key string, value string) {
	if k.lookup == nil {
		k.lookup = make(map[string][]string)
	}
	k.lookup[key] = append(k.lookup[key], value)
}

func (k *KeyToQueue) GetFirstVal(key string) (string, error) {
	if values, exists := k.lookup[key]; exists && len(values) > 0 {
		return values[0], nil
	}
	return "", errors.New("Not found.")
}

func (k *KeyToQueue) DeleteFirstVal(key string) error {
	if values, exists := k.lookup[key]; exists && len(values) > 0 {
		k.lookup[key] = values[1:]
		if len(k.lookup[key]) == 0 {
			delete(k.lookup, key)
		}
		return nil
	}
	return errors.New("Not found.")
}

func (k *KeyToQueue) DeleteKey(key string) {
	delete(k.lookup, key)
}

func (k *KeyToQueue) GetAllKeys() []string {
	var keys []string
	for key := range k.lookup {
		keys = append(keys, key)
	}
	return keys
}

func isOwnedByDaemonSet(pod *corev1.Pod, daemonset *appsv1.DaemonSet) bool {
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind == "DaemonSet" && ownerRef.Name == daemonset.Name {
			return true
		}
	}
	return false
}

var NodeAffinityLookup KeyToQueue
