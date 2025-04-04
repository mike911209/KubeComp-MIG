package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"knative.dev/client/pkg/commands"
	serving "knative.dev/client/pkg/serving/v1"
	kv1 "knative.dev/serving/pkg/apis/serving/v1"
)

type KnativeHelper struct {
	knativeClient serving.KnServingClient
}

func NewKnativeHelper(namespace string) *KnativeHelper {
	p := commands.KnParams{}
	p.Initialize()
	knativeClient, err := p.NewServingClient(namespace)
	if err != nil {
		log.Fatalf("Failed to create Knative client: %v", err)
	}
	return &KnativeHelper{
		knativeClient: knativeClient,
	}
}

func (k *KnativeHelper) ListRevisions(ctx context.Context, listOptions serving.ListConfig) (*kv1.RevisionList, error) {
	return k.knativeClient.ListRevisions(ctx, listOptions)
}

func (k *KnativeHelper) GetRevision(ctx context.Context, revisionName string) (*kv1.Revision, error) {
	return k.knativeClient.GetRevision(ctx, revisionName)
}

func (k *KnativeHelper) CreateRevision(ctx context.Context, revision *kv1.Revision) error {
	return k.knativeClient.CreateRevision(ctx, revision)
}

func (k *KnativeHelper) DeleteRevision(ctx context.Context, revisionName string, timeout time.Duration) error {
	// TODO check timeout
	return k.knativeClient.DeleteRevision(ctx, revisionName, timeout)
}

func (k *KnativeHelper) WaitRevision(ctx context.Context, revision *kv1.Revision, timeout time.Duration) error {
	log.Printf("Waiting for revision %s to be ready", revision.Name)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeStart := time.Now()
	for {
		<-ticker.C
		service, err := k.GetService(context.TODO(), revision.Labels["serving.knative.dev/service"])
		if err != nil {
			return err
		}
		if service.Status.LatestReadyRevisionName == revision.Name {
			log.Printf("Revision %s is ready", revision.Name)
			return nil
		}
		if time.Since(timeStart) > timeout {
			return fmt.Errorf("timeout waiting for revision %s to be ready", revision.Name)
		}
	}
}

func (k *KnativeHelper) ListServices(ctx context.Context) (*kv1.ServiceList, error) {
	return k.knativeClient.ListServices(ctx)
}

func (k *KnativeHelper) GetService(ctx context.Context, serviceName string) (*kv1.Service, error) {
	return k.knativeClient.GetService(ctx, serviceName)
}

func (k *KnativeHelper) UpdateService(ctx context.Context, service *kv1.Service) (bool, error) {
	return k.knativeClient.UpdateService(ctx, service)
}

func (k *KnativeHelper) WaitService(ctx context.Context, oldService *kv1.Service, timeout time.Duration) error {
	log.Printf("Waiting for service %s to be ready", oldService.Name)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeStart := time.Now()
	for {
		<-ticker.C
		service, err := k.GetService(context.TODO(), oldService.Name)
		if err != nil {
			return err
		}
		if service.Status.LatestReadyRevisionName != oldService.Status.LatestReadyRevisionName {
			log.Printf("Service %s is ready", service.Name)
			return nil
		}
		if time.Since(timeStart) > timeout {
			return fmt.Errorf("timeout waiting for service %s to be ready", service.Name)
		}
	}
}
