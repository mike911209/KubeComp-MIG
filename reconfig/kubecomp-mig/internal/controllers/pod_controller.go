package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type PodPreprocessReconciler struct {
	client.Client
	ClientSet *kubernetes.Clientset
	Ch        chan Pod
}

const (
	NodeAffinityLabel string = "expectedNode"
)

func (p *PodPreprocessReconciler) updateNodeAffinityLookup() {
	// remove non-exist entries
	keys := NodeAffinityLookup.GetAllKeys()
	for _, key := range keys {
		listOptions := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("pod-template-hash=%s", key),
		}
		replicaSetList, err := p.ClientSet.AppsV1().ReplicaSets("").List(context.TODO(), listOptions)
		if err != nil || len(replicaSetList.Items) == 0 {
			NodeAffinityLookup.DeleteKey(key)
		}
	}
}

func (p *PodPreprocessReconciler) preprocessHandler(po Pod) {
	log.Printf("Preprocess pod %s\n", po.name)
	pod, err := p.ClientSet.CoreV1().Pods(po.namespace).Get(context.Background(), po.name, metav1.GetOptions{})
	if err != nil {
		log.Printf("Error getting pod %s: %v", po.name, err)
		return
	}

	if NodeAffinityLookup.IsEmpty() {
		patchData := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]string{
					preprocessLabel: "done",
				},
			},
		}

		patchBytes, err := json.Marshal(patchData)
		_, err = p.ClientSet.CoreV1().Pods(po.namespace).Patch(
			context.Background(),
			po.name,
			types.StrategicMergePatchType,
			patchBytes,
			metav1.PatchOptions{},
		)
		if err != nil {
			log.Printf("Error when patch pod %s label: %v", po.name, err)
		}
		return
	}

	templateHash, exist := pod.Labels[podTemplateHash]
	if exist {
		node, err := NodeAffinityLookup.GetFirstVal(templateHash)
		if err == nil {
			pod.Labels[preprocessLabel] = "done"
			pod.Labels[NodeAffinityLabel] = node
			_, err = p.ClientSet.CoreV1().Pods(po.namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
			if err == nil {
				log.Printf("Pod %s's label %s=%s is updated\n", pod.Name, NodeAffinityLabel, node)
				NodeAffinityLookup.DeleteFirstVal(templateHash)
				return
			}
		}
	}
	p.updateNodeAffinityLookup()
	log.Printf("Put pod %s back to ch again.\n", pod.Name)
	p.Ch <- Pod{name: po.name, namespace: po.namespace}
}

func (p *PodPreprocessReconciler) Preprocess() {
	log.Printf("Preprocess starts\n")
	for {
		pod, ok := <-p.Ch
		if ok {
			p.preprocessHandler(pod)
		}
	}
}

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

	p.Ch <- Pod{name: req.Name, namespace: req.Namespace}

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
