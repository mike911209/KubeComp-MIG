package main

// Define the map that associates models with Docker images and commands
var ImageMap = map[string]map[string]string{
	"cuda": {
		"image":    "nvidia/cuda:12.5.1-runtime-ubi8",
		"command":  "nvidia-smi",
		"endpoint": "",
	},
	"test": {
		"image":    "ghcr.io/deeeelin/knative-service:latest",
		"command":  "",
		"endpoint": "",
	},
	"infer": { // theres a bug running this , maybe try remove "-"
		"image":    "ghcr.io/huggingface/text-generation-inference:2.2.0",
		"command":  "",
		"endpoint": ":8080/generate",
	},
}
