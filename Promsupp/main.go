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
	"promsupp",
}

type PromSupport struct{}

func (PS *PromSupport) operateService(clientset *kubernetes.Clientset, namespace string, svcName string, revisionName string, ownerReferences []metav1.OwnerReference, create bool) error {
	log.Printf("Creating Kubernetes service: %s-promservice", svcName)
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName + "-promservice",
			Namespace:       namespace,
			OwnerReferences: ownerReferences,
			Labels: map[string]string{
				"app": svcName,
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

	if create {
		_, err := clientset.CoreV1().Services(namespace).Create(context.TODO(), service, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("error creating service: %v", err)
		}
		log.Printf("Kubernetes Service for '%s' created successfully.\n", svcName)
	} else {
		_, err := clientset.CoreV1().Services(namespace).Update(context.TODO(), service, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("error updating service: %v", err)
		}
		log.Printf("Kubernetes Service for '%s' updated successfully.\n", svcName)
	}

	return nil
}

func (PS *PromSupport) createServiceMonitor(monitoringClient *monitoringclient.MonitoringV1Client, namespace string, svcName string, ownerReferences []metav1.OwnerReference) error {
	log.Printf("Creating Prometheus ServiceMonitor: %s-servicemonitor", svcName)

	// Define the ServiceMonitor object
	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName + "-servicemonitor",
			Namespace:       namespace,
			OwnerReferences: ownerReferences, // Set OwnerReferences if applicable
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": svcName,
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

	_, err := monitoringClient.ServiceMonitors(namespace).Create(context.Background(), serviceMonitor, metav1.CreateOptions{})

	if err != nil {
		return fmt.Errorf("error creating service monitor: %v", err)
	}
	log.Printf("Prometheus ServiceMonitor for '%s' created successfully.\n", svcName)
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
		services, err := client.ListServices(context.Background())
		if err != nil {
			log.Fatalf("Failed to list Knative services in namespace %s: %v", "default", err)
		}
		for _, svc := range services.Items { // iterate through all Knative services in namespace default
			// Check if the service name contains any of the ignore list items
			ignore := false
			for _, ignoreItem := range ignoreList {
				if strings.Contains(svc.Name, ignoreItem) {
					ignore = true
					break
				}
			}
			if ignore {
				log.Printf("Ignoring ksvc: %s", svc.Name)
				continue
			}

			log.Println("Checking knative service prom support: ", svc.Name)
			svcName := svc.Name
			svcUID := svc.UID
			ownerReferences := []metav1.OwnerReference{
				{
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
					Name:       svcName,
					UID:        svcUID,
				},
			}

			// Check if the service already exists
			k8ssvc, err := clientset.CoreV1().Services("default").Get(context.Background(), svcName+"-promservice", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					// Service does not exist, create it
					log.Printf("promservice not found , Creating promservice: %s-promservice", svcName)
					err = PS.operateService(clientset, "default", svcName, svc.Status.LatestReadyRevisionName, ownerReferences, true)
					if err != nil {
						log.Printf("Error creating service: %v", err)
					}
				} else {
					log.Printf("Error checking for service: %v", err)
				}
			} else if k8ssvc.Spec.Selector["app"] != svc.Status.LatestReadyRevisionName {
				// Service exists but selector is not updated
				log.Printf("Service %s has new revision, updating the service", svcName+"-promservice")
				err = PS.operateService(clientset, "default", svcName, svc.Status.LatestReadyRevisionName, ownerReferences, false)
				if err != nil {
					log.Printf("Error updating service: %v", err)
				}
			} else {
				log.Printf("Service %s already exists", svcName+"-promservice")
			}

			config, _ := rest.InClusterConfig()
			monitoringClient, _ := monitoringclient.NewForConfig(config)

			// Check if the ServiceMonitor already exists
			_, err = monitoringClient.ServiceMonitors("default").Get(context.Background(), svcName+"-servicemonitor", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					// ServiceMonitor does not exist, create it
					log.Println("ServiceMonitor not found , Creating ServiceMonitor: ", svcName+"-servicemonitor")
					err = PS.createServiceMonitor(monitoringClient, "default", svcName, ownerReferences)
					if err != nil {
						log.Printf("Error creating ServiceMonitor: %v", err)
					}
				} else {
					log.Printf("Error checking for ServiceMonitor: %v", err)
				}
			} else {
				log.Printf("ServiceMonitor %s already exists", svcName+"-servicemonitor")
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	PS := &PromSupport{}
	PS.Run()
}
