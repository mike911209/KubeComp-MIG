package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"net/url"
	"path/filepath"
)

const (
	targetPodLabel			string = "targetPod"
	targetNamespaceLabel	string = "targetNamespace"
	kubecompStatus			string = "kubecomp.com/reconfig.state"
	nvConfigLabel			string = "nvidia.com/mig.config"
	nvMigStateLabel			string = "nvidia.com/mig.config.state"
	migConfigPath			string = "/etc/config/config.yaml"
	migResources			string = "nvidia.com/mig"
)

type MigConfig struct {
	Devices    []int            `yaml:"devices"`
	MigEnabled bool             `yaml:"mig-enabled"`
	MigDevices map[string]int   `yaml:"mig-devices"`
}

type MigConfigYaml struct {
	Version     string `yaml:"version"`
	MigConfigs  map[string][]MigConfig `yaml:"mig-configs"`
}

type Pod struct {
	name string
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

func LocalEndpoint(path, file string) (string, error) {
	u := url.URL{
		Scheme: "unix",
		Path:   path,
	}
	return filepath.Join(u.String(), file+".sock"), nil
}