package main

import (
	"context"
	"os"
	"log"
	"time"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ctrl "sigs.k8s.io/controller-runtime"
	"k8s.io/client-go/kubernetes"
	pdrv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis/podresources"
	"path/filepath"
	"net/url"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvmlclient "kubecomp-mig/pkg/gpu"
	"kubecomp-mig/internal/reporter"
)

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

	// identify the node
	podName := os.Getenv("POD_NAME")
    podNamespace := os.Getenv("POD_NAMESPACE")
	pod, err := clientSet.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		log.Printf("error getting reconfig pod scheduled node: %v\n", err)
	}
	nodeName := pod.Spec.NodeName

	// create the Controller
	GPUReporter := reporter.GPUReporter{
		Client: manager.GetClient(), 
		ClientSet: clientSet, 
		Lister: lister, 
		NvmlClient: nvmlclient.NewClient(), 
		NodeName: nodeName,
	}
	GPUReporter.SetupWithManager(manager) 
	if err != nil {
		log.Fatal(err, "could not create GPUReporter")
	}

	// Start manager
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal(err, "could not start manager")
	}
}

