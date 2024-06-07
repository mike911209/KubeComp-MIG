package main

import (
	"os"
	"log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ctrl "sigs.k8s.io/controller-runtime"
	"k8s.io/client-go/kubernetes"

	nvmlclient "kubecomp-mig/pkg/gpu"
	"kubecomp-mig/internal/controllers"
)

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
	ReconfigReconciler := controllers.ReconfigReconciler{
		Client: manager.GetClient(), 
		Scheme: manager.GetScheme(), 
		ClientSet: clientSet, 
		NvmlClient: nvmlclient.NewClient(), 
	}
	ReconfigReconciler.SetupWithManager(manager) 
	if err != nil {
		log.Fatal(err, "could not create ReconfigReconciler")
	}

	podPreprocessReconciler := controllers.PodPreprocessReconciler{
		Client: manager.GetClient(), 
	}
	podPreprocessReconciler.SetupWithManager(manager) 
	if err != nil {
		log.Fatal(err, "could not create podPreprocessReconciler")
	}

	// Start manager
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal(err, "could not start manager")
	}
}

