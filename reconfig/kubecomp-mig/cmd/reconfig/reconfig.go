package main

import (
	"io/ioutil"
	"os"
	"log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ctrl "sigs.k8s.io/controller-runtime"
	"k8s.io/client-go/kubernetes"

	nvmlclient "kubecomp-mig/pkg/gpu"
	"kubecomp-mig/internal/controllers"
	"gopkg.in/yaml.v2"
)

const (
	migConfigPath			string = "/etc/config/config.yaml"
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

	// read mig-parted-config
	var migPartedConfig controllers.MigConfigYaml
	migConfigFile, err := ioutil.ReadFile(migConfigPath)
	if err != nil {
		log.Fatalf("Error reading mig config file: %v", err)
	}	
	err = yaml.Unmarshal(migConfigFile, &migPartedConfig)
	if err != nil {
		log.Fatalf("Error unmarshal mig config file: %v", err)
	}	

	// create the Controller
	ReconfigReconciler := controllers.ReconfigReconciler{
		Client: manager.GetClient(), 
		Scheme: manager.GetScheme(), 
		ClientSet: clientSet, 
		NvmlClient: nvmlclient.NewClient(), 
		MigPartedConfig: migPartedConfig,
	}
	ReconfigReconciler.SetupWithManager(manager) 
	if err != nil {
		log.Fatal(err, "could not create ReconfigReconciler")
	}

	podPreprocessReconciler := controllers.PodPreprocessReconciler{
		Client: manager.GetClient(), 
		ClientSet: clientSet, 
		Ch: make(chan controllers.Pod),
	}

	// Start preprocessor
	go podPreprocessReconciler.Preprocess()
	
	podPreprocessReconciler.SetupWithManager(manager) 
	if err != nil {
		log.Fatal(err, "could not create podPreprocessReconciler")
	}

	// Start manager
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal(err, "could not start manager")
	}
}

