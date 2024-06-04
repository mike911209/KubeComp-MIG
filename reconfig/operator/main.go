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
	pdrv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis/podresources"
	"path/filepath"
	"net/url"

	nvmlclient "reconfig/pkg/client"
	"gopkg.in/yaml.v2"
)

const (
	targetPodLabel			string = "targetPod"
	targetNamespaceLabel	string = "targetNamespace"
	kubecompStatus			string = "kubecomp.com/reconfig.state"
	nvConfigLabel			string = "nvidia.com/mig.config"
	nvMigStateLabel			string = "nvidia.com/mig.config.state"
	migConfigPath			string = "/etc/config/config.yaml"
	migResources			string = "nvidia.com/mig"
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

type Pod struct {
	name string
	namespace string
}

type LabelsChangedPredicate struct {
	predicate.Funcs
}

func (p LabelsChangedPredicate) Update(updateEvent event.UpdateEvent) bool {
	return !cmp.Equal(updateEvent.ObjectOld.GetLabels()[targetPodLabel], updateEvent.ObjectNew.GetLabels()[targetPodLabel]) ||
			!cmp.Equal(updateEvent.ObjectOld.GetLabels()[targetNamespaceLabel], updateEvent.ObjectNew.GetLabels()[targetNamespaceLabel]) 
}

func LocalEndpoint(path, file string) (string, error) {
	u := url.URL{
		Scheme: "unix",
		Path:   path,
	}
	return filepath.Join(u.String(), file+".sock"), nil
}

func initListerClient() (pdrv1.PodResourcesListerClient, error) {
	u := url.URL{
		Scheme: "unix",
		Path:   "/var/lib/kubelet/pod-resources",
	}
	endpoint := filepath.Join(u.String(), podresources.Socket+".sock")
	listerClient, _, err := podresources.GetV1Client(endpoint, 10 * time.Second, 1024 * 1024 * 16)
	return listerClient, err
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

	lister, err := initListerClient()
	if err != nil {
		log.Fatal(err, "could not create lister")
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
		Complete(&ReconfigReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme(), ClientSet: clientSet, Lister: lister, nvmlClient: nvmlclient.NewClient()})

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
	Lister pdrv1.PodResourcesListerClient
	nvmlClient	nvmlclient.ClientImpl
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
				if strings.HasPrefix(string(sliceName), migResources) {
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

func (r *ReconfigReconciler) getValidConfig(requestMigSlices map[string]int64) (string, error) {
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

func (r *ReconfigReconciler) getConfig(configName string) map[int]map[string]int {
	config := make(map[int]map[string]int)
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
		if profileName == configName {
			for _, deviceConfig := range migConfig {
				for _, d := range deviceConfig.Devices {
					config[d] = deviceConfig.MigDevices
				}
			}
		}
	}
	log.Printf("%s config: %v\n", configName, config)
	return config
}

func (r *ReconfigReconciler) getReconfigGPU(oldConfig string, newConfig string) []int {
	oldMigConfig := r.getConfig(oldConfig)
	newMigConfig := r.getConfig(newConfig)
	var gpuIDs []int

	for id, config := range newMigConfig {
		for key, val := range config {
			if val != oldMigConfig[id][key] {
				gpuIDs = append(gpuIDs, id)
				break
			}
		}
	}

	log.Printf("GPU %v will be reconfigured.\n", gpuIDs)
	return gpuIDs
}

func (r *ReconfigReconciler) stopPods(stopPods []Pod) {
	for _, pod := range stopPods {
		err := r.ClientSet.CoreV1().Pods(pod.namespace).Delete(context.Background(), pod.name, metav1.DeleteOptions{})
		if err != nil {
			log.Printf("Error deleting pod %s: %v", pod.name, err)
		}
		log.Printf("Delete pod %s in namespace %s\n", pod.name, pod.namespace)
	}

	// make sure the pods are successfully deleted
	for {
		deletedCnt := 0
		for _, pod := range stopPods {
			_, err := r.ClientSet.CoreV1().Pods(pod.namespace).Get(context.Background(), pod.name, metav1.GetOptions{})
			if err != nil {
				deletedCnt += 1
			}
		}
		if deletedCnt == len(stopPods) {
			break
		}
	}
}

type Status string
type Device struct {
	// ResourceName is the name of the resource exposed to k8s
	// (e.g. nvidia.com/gpu, nvidia.com/mig-2g10gb, etc.)
	ResourceName corev1.ResourceName
	// DeviceId is the actual ID of the underlying device
	// (e.g. ID of the GPU, ID of the MIG device, etc.)
	DeviceId string
	// Status represents the status of the k8s resource (e.g. free or used)
	Status Status
}

func (r *ReconfigReconciler) GetPodLocation(ctx context.Context) (map[int][]Pod, error) {
	podLocation := make(map[int][]Pod)

	listResp, err := r.Lister.List(ctx, &pdrv1.ListPodResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to list resources used by running Pods from Kubelet gRPC socket: %s", err)
	}

	for _, pr := range listResp.PodResources {
		pod := Pod{
			name: pr.Name,
			namespace: pr.Namespace,
		}
		for _, cr := range pr.Containers {
			for _, cd := range cr.GetDevices() {
				for _, cdId := range cd.DeviceIds {
					gpu, err := r.nvmlClient.GetMigDeviceGpuIndex(cdId)
					if err != nil {
						fmt.Printf("error when GetMigDeviceGpuIndex: %v\n", err)
					}
					podLocation[gpu] = append(podLocation[gpu], pod)
				}
			}
		}
	}

	return podLocation, nil
}

func (r *ReconfigReconciler) isResponsible(n *corev1.Node) bool{
	podName := os.Getenv("POD_NAME")
    podNamespace := os.Getenv("POD_NAMESPACE")
	pod, err := r.ClientSet.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		log.Printf("error getting reconfig pod scheduled node: %v\n", err)
	}
	return pod.Spec.NodeName == n.Name;
}

func (r *ReconfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := &corev1.Node{}
	err := r.Get(ctx, req.NamespacedName, node)
	if err != nil {
		return ctrl.Result{}, err
	}

	if (!r.isResponsible(node)) {
		return ctrl.Result{}, nil
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

	// adding taint for nodes
	taint := &corev1.Taint{
        Key:    kubecompStatus,
        Value:  "pending",
        Effect: corev1.TaintEffectNoSchedule,
    }
	node.Spec.Taints = append(node.Spec.Taints, *taint)
	
	err = r.Update(ctx, node)
	if err != nil {
		log.Printf("Error when adding taint to the node: %v", err)
	}

	/* test begin
	// delete the pod
	err = r.ClientSet.CoreV1().Pods(targetNamespace).Delete(context.Background(), targetPodName, metav1.DeleteOptions{})
	if err != nil {
		log.Printf("Error deleting pod: %v\n", err)
	}
	log.Printf("Delete pod %s in namespace %s\n", targetPodName, targetNamespace)
	// expect the deployment creates the pod but it cannot be scheduled
	time.Sleep(3 * time.Second)
	// try to edit the pod
	deployment, err := r.ClientSet.AppsV1().Deployments(targetNamespace).Get(context.TODO(), "mnist-train", metav1.GetOptions{})
	if err != nil {
		log.Printf("Error getting deployment: %v\n", err)
	}
	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	pods, err := r.ClientSet.CoreV1().Pods(targetNamespace).List(context.TODO(), metav1.ListOptions{
        LabelSelector: labelSelector,
    })
	if err != nil {
		log.Printf("Error listing pods: %v\n", err)
	}
	
	for _, pod := range pods.Items {
    	// Update the pod's labels
		pod.Labels["updated"] = "true"
		// Update the pod
		_, err = r.ClientSet.CoreV1().Pods(targetNamespace).Update(context.TODO(), &pod, metav1.UpdateOptions{})
		if err != nil {
			log.Printf("Error updating pods: %v\n", err)
		}
	}
	*/ 

	
	defer func() {
		err := r.Get(ctx, req.NamespacedName, node)
		var updatedTaints []corev1.Taint
		for _, taint := range node.Spec.Taints {
			if taint.Key != kubecompStatus {
				updatedTaints = append(updatedTaints, taint)
			}
		}
		node.Spec.Taints = updatedTaints

		err = r.Update(ctx, node)
		if err != nil {
			log.Printf("Error when removing label and taint: %v", err)
		}
		log.Printf("Leave reconcile.\n")
	}()

	// get the pod gpu location
	podLocation, err := r.GetPodLocation(ctx)
	if err != nil {
		log.Printf("error getting used device: %v\n", err)
		return ctrl.Result{}, nil
	}
	log.Printf("podLocation: %v\n", podLocation)

	// calculate the required resource
	usedSliceCnts, _ := r.extractUsedGPU(node.Name)

	// add the request of the target pod to the usedSliceCnts
	for _, c := range targetPod.Spec.Containers {
		for sliceName, sliceCnts := range c.Resources.Requests {
			if strings.HasPrefix(string(sliceName), migResources) {
				num, _ := sliceCnts.AsInt64()
				usedSliceCnts[string(sliceName)] += num
			}
		}
	}

	// check which config can handle the request
	log.Printf("Request Slices: %v\n", usedSliceCnts)
	updateConfig, err := r.getValidConfig(usedSliceCnts)
	if err != nil {
		log.Printf("Fail to get config: %v\n", err)
		return ctrl.Result{}, err
	}
	
	gpuIDs := r.getReconfigGPU(node.Labels[nvConfigLabel], updateConfig)
	var stopPods []Pod
	for _, id := range gpuIDs {
        stopPods = append(stopPods, podLocation[id]...)
    }
	stopPods = append(stopPods, Pod{name: targetPodName, namespace: targetNamespace})
	log.Printf("Reconfig for %s. Stop the pods %v\n", updateConfig, stopPods)
	r.stopPods(stopPods)
	
	// update label for gpu operator
	err = r.Get(ctx, req.NamespacedName, node)
	node.Labels[nvConfigLabel] = updateConfig
	err = r.Update(ctx, node)
	if err != nil {
		log.Fatalf("Error update nvConfigLabel: %v", err)
	}

	log.Printf("Wait for GPU operator...\n")
	for {
		time.Sleep(5 * time.Second)
		node, _ := r.ClientSet.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
		if node.Labels[nvMigStateLabel] != "pending" {
			break
		}
	}
	log.Printf("GPU operator is done.\n")

	return ctrl.Result{}, nil
}
