package main

// Define the map that associates models with Docker images and commands
var ImageMap = map[string]map[string]string{
	"test": {
		"image":   "ghcr.io/deeeelin/knative-service:latest",
		"command": "",
	},
	"tgi": { // theres a bug running this , maybe try remove "-"
		"image":   "ghcr.io/huggingface/text-generation-inference:2.2.0",
		"command": "",
	},
}
