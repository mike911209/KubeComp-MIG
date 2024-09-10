package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/client/pkg/kn/commands"
	servinglib "knative.dev/client/pkg/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
)

type Assigner struct{}

// create the service and forward the request
func (a *Assigner) AssignService(spec ServiceSpec, group RequestGroup) {
	log.Println("Assigning service based on the ServiceSpec")
	p := commands.KnParams{}
	p.Initialize()

	// Process each request in group to json payloads , store in array
	var packedRequests []io.ReadCloser
	for _, req := range group.Requests {
		packedRequest := a.CreatePayload(req)
		packedRequests = append(packedRequests, packedRequest)
		log.Printf("Packed request for token: %s", req.Token)
	}

	// Initialize the Knative serving client
	client, err := p.NewServingClient("default")
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	// List all services
	serviceList, err := client.ListServices(context.Background())
	if err != nil {
		log.Fatalf("Error listing Knative services: %s", err.Error())
	}

	// Check if the specified service name from spec exists
	serviceExists := false
	for _, svc := range serviceList.Items {
		log.Printf("Found service: %s", svc.Name)
		if svc.Name == spec.ServiceName {
			serviceExists = true
			break
		}
	}

	// There is a service running , just forward the payload
	if serviceExists {
		log.Printf("Service %s exists, updating the service", spec.ServiceName)
		a.CurrentService(spec, packedRequests)
	} else {
		// There isn't service running ,create a service and forward payload
		log.Printf("Service %s does not exist, creating a new service", spec.ServiceName)
		a.CreateNewService(spec, packedRequests)
	}
}

// CreatePayload creates a single json payload for a request
func (a *Assigner) CreatePayload(req Request) io.ReadCloser {
	log.Println("Creating payload for request")

	payload := map[string]interface{}{
		"inputs": req.Token,
		"parameters": map[string]interface{}{
			"max_new_tokens": 20,
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Error packing request: %s", err.Error())
	}

	log.Printf("Request Payload: %s", string(jsonPayload))
	return ioutil.NopCloser(bytes.NewBuffer(jsonPayload))
}

// CreateNewService creates a new service and forwards the request
func (a *Assigner) CreateNewService(spec ServiceSpec, requestPayloads []io.ReadCloser) {
	log.Printf("Creating new Knative service - ServiceName: %s", spec.ServiceName)
	p := commands.KnParams{}
	p.Initialize()

	// Create new knative serving client
	client, err := p.NewServingClient("default")
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	//Create a service instance
	var svcInstance = &servingv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.ServiceName,
			Namespace: "default",
		},
	}

	// Define resource requirements based on the spec
	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", spec.CPU)),     // Convert to millicores
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", spec.Memory)), // Memory in MiB
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", spec.CPU)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", spec.Memory)),
		},
	}

	// Add GPU resource request if specified on spec
	if len(spec.GPU_slices) > 0 {
		for sliceType, quantity := range spec.GPU_slices {
			resourceRequirements.Requests[v1.ResourceName(sliceType)] = resource.MustParse(fmt.Sprintf("%d", quantity))
			resourceRequirements.Limits[v1.ResourceName(sliceType)] = resource.MustParse(fmt.Sprintf("%d", quantity))
		}
	}

	// Select the image and command based on the spec.Model on Image Map
	selectedModel, exists := ImageMap[spec.Model]
	if !exists {
		log.Printf("Model %s not found in imageMap. Using default image and no command.", spec.Model)
		selectedModel = map[string]string{
			"image":   "ghcr.io/deeeelin/knative-service:latest",
			"command": "",
		}
	}

	image := selectedModel["image"]
	command := selectedModel["command"]
	fmt.Println(image, spec.Model)

	// Add all the resource requirements define to service instance
	svcInstance.Spec.Template = servingv1.RevisionTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				servinglib.UserImageAnnotationKey: "",
			},
		},
		Spec: servingv1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image:           image,
					ImagePullPolicy: corev1.PullAlways,
					Resources:       resourceRequirements,
				}},
			},
		},
	}

	if command != "" {
		log.Printf("Setting command: %s", command)
		svcInstance.Spec.Template.Spec.PodSpec.Containers[0].Command = []string{command}
	}

	// Use the service instance to create service
	ctx := context.Background()
	err = client.CreateService(ctx, svcInstance)
	if err != nil {
		log.Fatalf("Error creating Knative service: %s", err.Error())
	}

	// wait for service be ready and forward payload
	go a.waitForServiceReadyAndForward(spec, requestPayloads)
}

// forwards the requests to existing service
func (a *Assigner) CurrentService(spec ServiceSpec, requestPayloads []io.ReadCloser) {
	selectedEndpoint, exists := ImageMap[spec.Model]["endpoint"]
	if !exists {
		log.Printf("Endpoint of model %s not found in imageMap. Set as empty", spec.Model)
		selectedEndpoint = ""
	}

	// Forward each request one by one
	for _, requestPayload := range requestPayloads {
		go a.forwardRequest(fmt.Sprintf("http://%s.default.svc.cluster.local"+selectedEndpoint, spec.ServiceName), requestPayload)
	}
}

// wait for service be ready and forward payload
func (a *Assigner) waitForServiceReadyAndForward(spec ServiceSpec, requestPayloads []io.ReadCloser) {
	log.Printf("Waiting for service to be ready - ServiceName: %s", spec.ServiceName)
	selectedEndpoint, exists := ImageMap[spec.Model]["endpoint"]
	if !exists {
		log.Printf("Endpoint of model %s not found in imageMap. Set as empty", spec.Model)
		selectedEndpoint = ""
	}

	p := commands.KnParams{}
	p.Initialize()
	knClient, err := p.NewServingClient("default")
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	ctx := context.Background()
	for {
		service, err := knClient.GetService(ctx, spec.ServiceName)
		if err != nil {
			log.Fatalf("Error getting Knative service: %s", err.Error())
		}

		// wait for service to be ready
		for _, condition := range service.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				log.Printf("Knative Service is ready - ServiceName: %s", spec.ServiceName)
				// Forward each request payload of the request group one by one
				for _, requestPayload := range requestPayloads {
					a.forwardRequest(fmt.Sprintf("http://%s.default.svc.cluster.local"+selectedEndpoint, spec.ServiceName), requestPayload)
					log.Printf("Forwarding request to %s", fmt.Sprintf("http://%s.default.svc.cluster.local"+selectedEndpoint, spec.ServiceName))
				}
				return
			}
		}

		log.Println("Waiting for the Knative service to be ready...")
		time.Sleep(1 * time.Second)
	}
}

// forward a request to the service
func (a *Assigner) forwardRequest(serviceURL string, requestPayload io.ReadCloser) {
	log.Printf("Forwarding request to service URL: %s", serviceURL)

	payload, err := ioutil.ReadAll(requestPayload)
	if err != nil {
		log.Printf("Error reading request payload: %s", err.Error())
		return
	}

	resp, err := http.Post(serviceURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Failed to forward request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// receive any response, print it out
	respPayload, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response: %v\n", err)
		return
	}
	log.Printf("Response Payload from service: %s", string(respPayload))

	log.Println("Response sent back to original sender")
}
