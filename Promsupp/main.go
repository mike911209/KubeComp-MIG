package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringclient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned/typed/monitoring/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"knative.dev/client/pkg/commands"
)

var ignoreList = []string{
	"dispatcher",
	"autoscaler",
	"promsupp",
}

type PromSupport struct{}

func (PS *PromSupport) createService(clientset *kubernetes.Clientset, namespace string, revisionName string, ownerReferences []metav1.OwnerReference) error {
	log.Printf("Creating Kubernetes service: %s-promservice", revisionName)
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            revisionName + "-promservice",
			Namespace:       namespace,
			OwnerReferences: ownerReferences,
			Labels: map[string]string{
				"app": revisionName,
			},
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"app": revisionName,
			},
			Ports: []v1.ServicePort{
				{
					Name:     "metrics",
					Port:     8080,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}

	_, err := clientset.CoreV1().Services(namespace).Create(context.TODO(), service, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating service: %v", err)
	}
	log.Printf("Kubernetes Service for '%s' created successfully.\n", revisionName)
	return nil
}

func (PS *PromSupport) createServiceMonitor(clientset *kubernetes.Clientset, namespace string, revisionName string, ownerReferences []metav1.OwnerReference) error {
	log.Printf("Creating Prometheus ServiceMonitor: %s-servicemonitor", revisionName)

	// Define the ServiceMonitor object
	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:            revisionName + "-servicemonitor",
			Namespace:       namespace,
			OwnerReferences: ownerReferences, // Set OwnerReferences if applicable
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": revisionName,
				},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     "metrics",
					Interval: "10s",
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				Any:        false,
				MatchNames: []string{},
			},
		},
	}

	config, _ := rest.InClusterConfig()
	monitoringClient, err := monitoringclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error creating monitoring client: %v", err)
	}

	_, err = monitoringClient.ServiceMonitors(namespace).Create(context.Background(), serviceMonitor, metav1.CreateOptions{})

	if err != nil {
		return fmt.Errorf("error creating service monitor: %v", err)
	}
	log.Printf("Prometheus ServiceMonitor for '%s' created successfully.\n", revisionName)
	return nil
}

func (PS *PromSupport) Run() {

	p := commands.KnParams{}
	p.Initialize()
	// Create new knative serving client
	client, err := p.NewServingClient("default")
	if err != nil {
		log.Fatalf("Error creating Knative serving client: %s", err.Error())
	}
	config, _ := rest.InClusterConfig()
	clientset, _ := kubernetes.NewForConfig(config)

	for { // check within interval time

		revisions, err := client.ListRevisions(context.Background())
		if err != nil {
			log.Fatalf("Failed to list Knative services in namespace %s: %v", "default", err)
		}
		for _, revision := range revisions.Items { // iterate through all Knative services in namespace default
			// Check if the revision name contains any of the ignore list items
			ignore := false
			for _, ignoreItem := range ignoreList {
				if strings.Contains(revision.Name, ignoreItem) {
					ignore = true
					break
				}
			}
			if ignore {
				log.Printf("Ignoring revision: %s", revision.Name)
				continue
			}

			log.Println("Checking knative revision prom support : ", revision.Name)
			revisionName := revision.Name
			revisionUID := revision.UID
			ownerReferences := []metav1.OwnerReference{
				{
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Revision",
					Name:       revisionName,
					UID:        revisionUID,
				},
			}

			// Check if the service already exists
			_, err := clientset.CoreV1().Services("default").Get(context.Background(), revisionName+"-promservice", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					// Service does not exist, create it
					log.Printf("promservice not found , Creating promservice: %s-promservice", revisionName)
					err = PS.createService(clientset, "default", revisionName, ownerReferences)
					if err != nil {
						log.Printf("Error creating service: %v", err)
					}
				} else {
					log.Printf("Error checking for service: %v", err)
				}
			} else {
				log.Printf("Service %s already exists", revisionName+"-promservice")
			}

			config, _ := rest.InClusterConfig()
			monitoringClient, err := monitoringclient.NewForConfig(config)

			// Check if the ServiceMonitor already exists
			_, err = monitoringClient.ServiceMonitors("default").Get(context.Background(), revisionName+"-servicemonitor", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					// ServiceMonitor does not exist, create it
					log.Println("ServiceMonitor not found , Creating ServiceMonitor: ", revisionName+"-servicemonitor")
					err = PS.createServiceMonitor(clientset, "default", revisionName, ownerReferences)
					if err != nil {
						log.Printf("Error creating ServiceMonitor: %v", err)
					}
				} else {
					log.Printf("Error checking for ServiceMonitor: %v", err)
				}
			} else {
				log.Printf("ServiceMonitor %s already exists", revisionName+"-servicemonitor")
			}
		}
		time.Sleep(1)
	}
}

func main() {
	PS := &PromSupport{}
	PS.Run()
}
