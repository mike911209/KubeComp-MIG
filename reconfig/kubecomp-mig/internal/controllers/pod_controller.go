package controllers

import (
	"context"
	"log"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type PodPreprocessReconciler struct {
	client.Client
}

const (
	maxRetries 			int 			= 5
	retryInterval 		time.Duration	= time.Second * 1
	NodeAffinityLabel 	string 			= "expectedNode"
)

func (p *PodPreprocessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &corev1.Pod{}
	objKey := client.ObjectKey{Namespace: req.Namespace, Name: req.Name}
	err := p.Get(ctx, objKey, pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if _, exists := pod.Labels[preprocessLabel]; exists {
		return ctrl.Result{}, err
	}

	templateHash, exist := pod.Labels[podTemplateHash]
	if exist {
		node, err := NodeAffinityLookup.GetFirstVal(templateHash)
		if err == nil {
			// update
			retryCount := 0
			for {
				_ = p.Get(ctx, objKey, pod)
				pod.Labels[NodeAffinityLabel] = node
				err = p.Update(ctx, pod) // change to update the label

				if err == nil {
					log.Printf("Pod %s's label %s=%s is updated\n", pod.Name, NodeAffinityLabel, node)
					NodeAffinityLookup.DeleteFirstVal(templateHash)
					break
				}
				if errors.IsConflict(err) {
					if retryCount < maxRetries {
						retryCount++
						time.Sleep(retryInterval)
					} else {
						log.Printf("Error when adding label %s=%s for pod %s: %v", NodeAffinityLabel, node, pod.Name, err)
						break
					}
				} else {
					log.Printf("Error: %v", err)
					break
				}
			}
		}
	}

	// update
	retryCount := 0
	for {
		_ = p.Get(ctx, objKey, pod)
		pod.Labels[preprocessLabel] = "done"
		err = p.Update(ctx, pod)

		if err == nil {
			log.Printf("Pod %s is preprocessed\n", pod.Name)
			break
		}
		
		if errors.IsConflict(err) {
			if retryCount < maxRetries {
				retryCount++
				time.Sleep(retryInterval)
				continue
			} else {
				log.Printf("Error when adding preprocessLabel to pod: %v", err)
			}
		} else {
			log.Printf("Error: %v", err)
			break
		}
	}
	return ctrl.Result{}, err
}
func (p *PodPreprocessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(predicate.Funcs{
            CreateFunc: func(e event.CreateEvent) bool {
                return true
            },
            UpdateFunc: func(e event.UpdateEvent) bool {
                return false
            },
            DeleteFunc: func(e event.DeleteEvent) bool {
                return false
            },
            GenericFunc: func(e event.GenericEvent) bool {
                return false
            },
        }).
		Complete(p)
}