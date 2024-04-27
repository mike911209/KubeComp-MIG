package main

import (
	"context"
	"os"
	"io/ioutil"
	"log"
	"strings"
	"time"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	corev1 "k8s.io/api/core/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"github.com/google/go-cmp/cmp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gopkg.in/yaml.v2"
)

const (
	targetPodLabel			string = "targetPod"
	targetNamespaceLabel	string = "targetNamespace"
	podRestart				string = "restart"
	kubecompStatus			string = "kubecomp.com/reconfig.state"
	nvConfigLabel			string = "nvidia.com/mig.config"
	nvMigStateLabel			string = "nvidia.com/mig.config.state"
	migConfigPath			string = "/etc/config/config.yaml"
	gpuResources			string = "nvidia.com/"
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

type LabelsChangedPredicate struct {
	predicate.Funcs
}

func (p LabelsChangedPredicate) Update(updateEvent event.UpdateEvent) bool {
	return !cmp.Equal(updateEvent.ObjectOld.GetLabels()[targetPodLabel], updateEvent.ObjectNew.GetLabels()[targetPodLabel]) ||
			!cmp.Equal(updateEvent.ObjectOld.GetLabels()[targetNamespaceLabel], updateEvent.ObjectNew.GetLabels()[targetNamespaceLabel]) 
}

func main() {
	opts := zap.Options{}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cfg := ctrl.GetConfigOrDie()
	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatal("unable to create clientset")
	}

	manager, err := ctrl.NewManager(cfg, ctrl.Options{})
	if err != nil {
		log.Fatal(err, "could not create manager")
		os.Exit(1)
	}

	// create the Controller
	err = ctrl.
		NewControllerManagedBy(manager). 
		For(
			&corev1.Node{},
			builder.WithPredicates(
				LabelsChangedPredicate{},
			),
		).       
		Complete(&ReconfigReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme(), ClientSet: clientSet})

	if err != nil {
		log.Fatal(err, "could not create controller")
	}

	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal(err, "could not start manager")
	}
}

type ReconfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	ClientSet *kubernetes.Clientset
}

func (r *ReconfigReconciler) extractUsedGPU(nodeName string) (map[string]int64, []corev1.Pod) {
	migSliceCnts := make(map[string]int64)
	var gpuPods []corev1.Pod

	// list all the pods scheduled on the node
	pods, err := r.ClientSet.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
        FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
    })
	if err != nil {
		log.Fatal("Error listing pods on node %s: %v\n", nodeName, err)
	}

	for _, pod := range pods.Items {
		// skip the terminated pod
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		addToGpuPods := false
		for _, c := range pod.Spec.Containers {
			for sliceName, sliceCnts := range c.Resources.Requests {
				if strings.HasPrefix(string(sliceName), gpuResources) {
					num, _ := sliceCnts.AsInt64()
					migSliceCnts[string(sliceName)] += num
					if !addToGpuPods {
						gpuPods = append(gpuPods, pod)
						addToGpuPods = true
					}
				}
			}
		}
	}
	return migSliceCnts, gpuPods
}

func (r *ReconfigReconciler) getConfig(requestMigSlices map[string]int64) (string, error) {
	configFile, err := ioutil.ReadFile(migConfigPath)

	if err != nil {
		log.Fatalf("Error reading mig config file: %v", err)
	}	

	var migConfigYaml MigConfigYaml
	err = yaml.Unmarshal(configFile, &migConfigYaml)
	if err != nil {
		log.Fatalf("Error unmarshaling YAML: %v", err)
	}

	for profileName, migConfig := range migConfigYaml.MigConfigs {
		find := true
		log.Printf("Check profile %s\n", profileName)
		for requestMigSlice, requestMigCnt := range requestMigSlices {
			cnt := int64(0)
			for _, deviceConfig := range migConfig {
				removeString := "nvidia.com/mig-"
				sliceName := requestMigSlice[len(removeString):]
				cnt += int64(deviceConfig.MigDevices[sliceName] * len(deviceConfig.Devices))
			}
			log.Printf("%s: %d\n", requestMigSlice, cnt)
			if cnt < requestMigCnt {
				find = false
				break
			}
		}
		if find {
			return profileName, nil
		}
	}
	return "", fmt.Errorf("Config not found.")
}

func (r *ReconfigReconciler) stopPods(stopPods []corev1.Pod) {
	for _, pod := range stopPods {
		err := r.ClientSet.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Printf("Error deleting pod %s: %v", pod.Name, err)
		}
		log.Printf("Delete pod %s in namespace %s\n", pod.Name, pod.Namespace)
	}

	// make sure the pods are successfully deleted
	for {
		deletedCnt := 0
		for _, pod := range stopPods {
			_, err := r.ClientSet.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err != nil {
				deletedCnt += 1
			}
		}
		if deletedCnt == len(stopPods) {
			break
		}
	}
}

func (r *ReconfigReconciler) startPods(stopPods []corev1.Pod) {
	for _, pod := range stopPods {
		newPod := &corev1.Pod{}
		newPod.SetName(pod.Name)
		newPod.SetNamespace(pod.Namespace)
		if newPod.Labels == nil {
			newPod.Labels = make(map[string]string)
		}
		newPod.Labels[podRestart] = "true"
		newPod.Spec = pod.Spec
		newPod.Spec.NodeName = ""
		_, err := r.ClientSet.CoreV1().Pods(pod.Namespace).Create(context.Background(), newPod, metav1.CreateOptions{})
		if err != nil {
			log.Printf("Error creating pod %s: %v", newPod.Name, err)
		}
		log.Printf("Create pod %s in namespace %s\n", newPod.Name, newPod.Namespace)
	}

	// make sure the pods are successfully created
	for {
		createdCnt := 0
		for _, pod := range stopPods {
			p, err := r.ClientSet.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err == nil && p.Status.Phase != corev1.PodPending {
				createdCnt += 1
			}
		}
		if createdCnt == len(stopPods) {
			break
		}
	}
}

func (r *ReconfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := &corev1.Node{}

	// label the reconfig status
	err := r.Get(ctx, req.NamespacedName, node)
	if err != nil {
		return ctrl.Result{}, err
	}

	// identify the target pod
	targetPodName := node.Labels[targetPodLabel]
	targetNamespace := node.Labels[targetNamespaceLabel]
	targetPod, err := r.ClientSet.CoreV1().Pods(targetNamespace).Get(context.TODO(), targetPodName, metav1.GetOptions{})
	if err != nil {
		log.Printf("Namespace %s, Name %s not found\n", targetNamespace, targetPodName)
		return ctrl.Result{}, nil
	}
	if targetPod.Spec.NodeName != "" {
		log.Printf("Namespace %s, Name %s is scheduled\n", targetNamespace, targetPodName)
		return ctrl.Result{}, nil
	}

	// reconfig starts
	log.Printf("Reconfig for pod %s in namespace %s\n", targetPodName, targetNamespace)
	node.Labels[kubecompStatus] = "pending"
	err = r.Update(ctx, node)
	if err != nil {
		log.Fatalf("Error update kubecompStatus to pending: %v", err)
	}
	defer func() {
		err := r.Get(ctx, req.NamespacedName, node)
		node.Labels[kubecompStatus] = "done"
		err = r.Update(ctx, node)
		if err != nil {
			log.Fatalf("Error update kubecompStatus to done: %v", err)
		}
		log.Printf("Leave reconcile\n")
	}()

	// calculate the required resource
	usedSliceCnts, gpuPods := r.extractUsedGPU(node.Name)

	// add the request of the target pod to the usedSliceCnts
	for _, c := range targetPod.Spec.Containers {
		for sliceName, sliceCnts := range c.Resources.Requests {
			if strings.HasPrefix(string(sliceName), gpuResources) {
				num, _ := sliceCnts.AsInt64()
				usedSliceCnts[string(sliceName)] += num
			}
		}
	}

	// check which config can handle the request
	log.Printf("Request Slices: %v\n", usedSliceCnts)
	updateConfig, err := r.getConfig(usedSliceCnts)
	if err != nil {
		log.Printf("Fail to get config: %v\n", err)
		return ctrl.Result{}, err
	}
	
	log.Printf("Reconfig for %s. Stop the pods...\n", updateConfig)
	gpuPods = append(gpuPods, *targetPod)
	r.stopPods(gpuPods)
	
	// update label for gpu operator
	err = r.Get(ctx, req.NamespacedName, node)
	node.Labels[nvConfigLabel] = updateConfig
	err = r.Update(ctx, node)
	if err != nil {
		log.Fatalf("Error update nvConfigLabel: %v", err)
	}

	log.Printf("Label update. Wait for GPU operator...\n")
	for {
		time.Sleep(5 * time.Second)
		node, _ := r.ClientSet.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
		if node.Labels[nvMigStateLabel] != "pending" {
			break
		}
	}

	log.Printf("Reconfig completes. Start the pods...\n")
	r.startPods(gpuPods)
	
	return ctrl.Result{}, nil
}
