package reporter

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	nvmlclient "kubecomp-mig/pkg/gpu"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	pdrv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	gpuIDLabel    string        = "gpuIDs"
	migResources  string        = "nvidia.com/mig"
	maxRetries    int           = 5
	retryInterval time.Duration = time.Second * 1
)

type GPUReporter struct {
	client.Client
	ClientSet  *kubernetes.Clientset
	Lister     pdrv1.PodResourcesListerClient
	NvmlClient nvmlclient.ClientImpl
	NodeName   string
}

func (r *GPUReporter) updateNodeResourceLabel(ctx context.Context) {
	log.Printf("updateNodeResourceLabel\n")
	mig_info, err := r.NvmlClient.GetAllMigs()
	if err != nil {
		log.Printf("error: %v.\n", err)
	}

	listResp, err := r.Lister.List(ctx, &pdrv1.ListPodResourcesRequest{})
	if err != nil {
		log.Printf("unable to list resources used by running Pods from Kubelet gRPC socket: %s", err)
		return
	}
	for _, pr := range listResp.PodResources {
		for _, cr := range pr.Containers {
			for _, cd := range cr.GetDevices() {
				for _, cdId := range cd.DeviceIds {
					delete(mig_info, cdId)
				}
			}
		}
	}

	gpuResources := make(map[int]map[string]int)

	for _, info := range mig_info {
		if gpuResources[info.GpuID] == nil {
			gpuResources[info.GpuID] = make(map[string]int)
		}
		gpuResources[info.GpuID][info.Profile]++
	}

	node, err := r.ClientSet.CoreV1().Nodes().Get(context.TODO(), r.NodeName, metav1.GetOptions{})
	if err != nil {
		log.Printf("unable to get node %s: %v", r.NodeName, err)
		return
	}

	for key := range node.Labels {
		if strings.HasPrefix(key, "kubecomp/status-gpu") {
			delete(node.Labels, key)
		}
	}

	for gpuID, profiles := range gpuResources {
		for profile, count := range profiles {
			resourceKey := fmt.Sprintf("kubecomp/status-gpu-%d-%s-free", gpuID, profile)
			resourceCount := strconv.Itoa(count)
			node.Labels[resourceKey] = resourceCount
		}
	}

	_, err = r.ClientSet.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil {
		fmt.Printf("Failed to update node label: %v\n", err)
	} else {
		fmt.Printf("gpuResources: %v\n", gpuResources)
		fmt.Printf("Node is updated successfully: %v\n", node.Labels)
	}
}

func (r *GPUReporter) requestGPU(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		for sliceName := range c.Resources.Requests {
			if strings.HasPrefix(string(sliceName), migResources) {
				return true
			}
		}
	}
	return false
}

func (r *GPUReporter) getGPUIDs(ctx context.Context, pod *corev1.Pod) (string, error) {
	listResp, err := r.Lister.List(ctx, &pdrv1.ListPodResourcesRequest{})
	if err != nil {
		return "", fmt.Errorf("unable to list resources used by running Pods from Kubelet gRPC socket: %s", err)
	}
	for _, pr := range listResp.PodResources {
		gpuIDs := []string{}
		if pr.Name != pod.Name || pr.Namespace != pod.Namespace {
			continue
		}
		for _, cr := range pr.Containers {
			for _, cd := range cr.GetDevices() {
				for _, cdId := range cd.DeviceIds {
					gpu, err := r.NvmlClient.GetMigDeviceGpuIndex(cdId)
					if err != nil {
						return "", fmt.Errorf("unable to get GPU used by the pod")
					}
					gpuIDs = append(gpuIDs, strconv.Itoa(gpu))
				}
			}
		}
		return strings.Join(gpuIDs, ","), nil
	}
	return "", fmt.Errorf("unable to get GPU used by the pod")
}

func (r *GPUReporter) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &corev1.Pod{}
	objKey := client.ObjectKey{Namespace: req.Namespace, Name: req.Name}

	defer r.updateNodeResourceLabel(ctx)

	err := r.Get(ctx, objKey, pod)
	if err != nil || pod.Spec.NodeName != r.NodeName || !r.requestGPU(pod) {
		return ctrl.Result{}, nil
	}

	if pod.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, err
	}

	if !r.requestGPU(pod) {
		return ctrl.Result{}, err
	}

	log.Printf("Reconcile is triggered because of pod %s.\n", pod.Name)
	gpuIDs := ""
	retryCount := 0
	for {
		gpuIDs, err = r.getGPUIDs(ctx, pod)
		if err != nil {
			if retryCount < maxRetries {
				retryCount++
				time.Sleep(retryInterval)
			} else {
				return ctrl.Result{}, err
			}
		} else {
			break
		}
	}

	err = r.Get(ctx, objKey, pod)
	pod.Labels[gpuIDLabel] = gpuIDs
	err = r.Update(ctx, pod)
	log.Printf("Add label %s: %s\n", gpuIDLabel, gpuIDs)
	return ctrl.Result{}, err
}

func (r *GPUReporter) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldPod, okOld := e.ObjectOld.(*corev1.Pod)
				newPod, okNew := e.ObjectNew.(*corev1.Pod)
				if !okOld || !okNew {
					return false
				}
				return oldPod.Spec.NodeName != newPod.Spec.NodeName
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return true
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
		}).
		Complete(r)
}
