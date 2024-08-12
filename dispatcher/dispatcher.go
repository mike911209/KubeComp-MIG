package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"knative.dev/client/pkg/kn/commands"
	servinglib "knative.dev/client/pkg/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
)

type ServiceSpec struct {
	Token     string            `json:"token"`
	TokenSize int               `json:"token_size"`
	Model     string            `json:"model"`
	SLO       int               `json:"slo"`
	Headers   map[string]string `json:"headers"`
}

type RequestPayload struct {
	Label string `json:"label"`
	Msg   string `json:"msg"`
}

func main() {
	http.HandleFunc("/", handleRequest)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received request")

	// Print all headers
	for name, values := range r.Header {
		for _, value := range values {
			fmt.Printf("Header: %s, Value: %s\n", name, value)
		}
	}

	token := r.Header.Get("token")
	if token == "" {
		http.Error(w, "Token header is missing", http.StatusBadRequest)
		return
	}
	tokenSize := len(token)
	fmt.Printf("Token: %s, TokenSize: %d\n", token, tokenSize)

	model := r.Header.Get("model")
	if model == "" {
		http.Error(w, "Model header is missing", http.StatusBadRequest)
		return
	}
	fmt.Printf("Model: %s\n", model)

	sloStr := r.Header.Get("slo")
	if sloStr == "" {
		http.Error(w, "SLO header is missing", http.StatusBadRequest)
		return
	}
	slo, err := strconv.Atoi(sloStr)
	if err != nil {
		http.Error(w, "Invalid SLO header value", http.StatusBadRequest)
		return
	}
	fmt.Printf("SLO: %d\n", slo)

	spec := ServiceSpec{
		Token:     token,
		TokenSize: tokenSize,
		Model:     model,
		SLO:       slo,
	}
	fmt.Printf("ServiceSpec: %+v\n", spec)

	serviceURL := decideService(spec)
	fmt.Printf("Service URL: %s\n", serviceURL)

	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()
	fmt.Printf("Request Payload: %s\n", string(payload))

	var requestPayload RequestPayload
	if err := json.Unmarshal(payload, &requestPayload); err != nil {
		http.Error(w, "Failed to parse request payload", http.StatusInternalServerError)
		return
	}
	fmt.Printf("Parsed Request Payload: %+v\n", requestPayload)

	forwardRequest(serviceURL, payload, w)
}

func decideService(spec ServiceSpec) string {
	fmt.Println("Deciding service")
	// This function should include logic to decide if an appropriate service already exists.
	// As a placeholder, it returns an empty string indicating that a new service should be created.
	serviceURL := createNewService(spec)
	fmt.Printf("Created service URL: %s\n", serviceURL)
	return serviceURL
}

func createNewService(spec ServiceSpec) string {
	fmt.Println("Creating new service")
	p := commands.KnParams{}
	p.Initialize()

	client, err := p.NewServingClient("default")
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}

	serviceName := "test"
	namespace := "default"
	fmt.Printf("Service Name: %s\n", serviceName)

	var svcInstance = &servingv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
	}

	svcInstance.Spec.Template = servingv1.RevisionTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				servinglib.UserImageAnnotationKey: "",
			},
		},
		Spec: servingv1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "ghcr-login-secret"},
				},
				Containers: []corev1.Container{{
					Image:           spec.Model,
					ImagePullPolicy: corev1.PullAlways,
				}},
			},
		},
	}
	ctx := context.Background()
	err = client.CreateService(ctx, svcInstance)
	if err != nil {
		log.Fatalf("Error creating Knative service: %s", err.Error())
	}

	// Create Kubernetes clientset
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error creating in-cluster config: %s", err.Error())
	}
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes clientset: %s", err.Error())
	}

	// Wait for the service to be ready and get its URL
	for {
		updatedService, err := client.GetService(ctx, serviceName)
		if err != nil {
			log.Fatalf("Error getting Knative service: %s", err.Error())
		}

		url := updatedService.Status.URL
		if url != nil {
			// Check if the service is ready
			if updatedService.Status.GetCondition(servingv1.ServiceConditionReady).IsTrue() {
				// Check if the container is running
				podList, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("serving.knative.dev/service=%s", serviceName),
				})
				if err != nil {
					log.Fatalf("Error listing pods for service: %s", err.Error())
				}

				for _, pod := range podList.Items {
					for _, containerStatus := range pod.Status.ContainerStatuses {
						if containerStatus.State.Running != nil {
							fmt.Printf("Service is ready with URL: %s\n", url.String())
							return fmt.Sprintf("http://%s.%s.svc.cluster.local", serviceName, namespace)
						}
					}
				}
			}
		}

		fmt.Println("Waiting for the service to be ready and container to be running...")
		time.Sleep(1 * time.Second)
	}
}

func forwardRequest(serviceURL string, payload []byte, w http.ResponseWriter) {
	fmt.Printf("Forwarding request to service URL: %s\n", serviceURL)
	resp, err := http.Post(serviceURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Failed to forward request: %v\n", err)
		http.Error(w, "Failed to forward request", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	respPayload, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response: %v\n", err)
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}
	fmt.Printf("Response Payload from service: %s\n", string(respPayload))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respPayload)
	fmt.Println("Response sent back to original sender")
}
