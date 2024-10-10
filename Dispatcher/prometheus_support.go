package main

import (
	"context"
	"fmt"
	"log"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringclient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned/typed/monitoring/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type PromSupport struct{}

func (PS *PromSupport) createService(clientset *kubernetes.Clientset, namespace string, serviceName string, ownerReferences []metav1.OwnerReference) error {
	log.Printf("Creating Kubernetes service: %s-promservice", serviceName)
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            serviceName + "-00001" + "-promservice",
			Namespace:       namespace,
			OwnerReferences: ownerReferences,
			Labels: map[string]string{
				"app": serviceName + "-00001",
			},
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"app": serviceName + "-00001",
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
	log.Printf("Kubernetes Service for '%s' created successfully.\n", serviceName)
	return nil
}

func (PS *PromSupport) createServiceMonitor(clientset *kubernetes.Clientset, namespace string, serviceName string, ownerReferences []metav1.OwnerReference) error {
	log.Printf("Creating Prometheus ServiceMonitor: %s-servicemonitor", serviceName)

	// Define the ServiceMonitor object
	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:            serviceName + "-00001" + "-servicemonitor",
			Namespace:       namespace,
			OwnerReferences: ownerReferences, // Set OwnerReferences if applicable
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": serviceName + "-00001",
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
	log.Printf("Prometheus ServiceMonitor for '%s' created successfully.\n", serviceName)
	return nil
}
