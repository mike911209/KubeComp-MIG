# Variables
IMAGE_NAME=autoscaler
IMAGE_TAG=local
REGISTRY=ghcr.io/mike911209
KUBE_CONFIG=configuration.yaml

# Build Docker image
build:
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) --push .

# Apply Kubernetes configuration
deploy:
	kubectl apply -f $(KUBE_CONFIG)

# Clean up Kubernetes resources
clean:
	kubectl delete -f $(KUBE_CONFIG)

# Full pipeline: build, push, and deploy
all: build deploy

.PHONY: build deploy clean all
