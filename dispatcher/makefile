# Variables
IMAGE_NAME=dispatcher
IMAGE_TAG=latest
KUBE_CONFIG=configuration.yaml

.PHONY: all build push deploy clean

all: build deploy

buildx:
	docker buildx create --use
	docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/deeeelin/$(IMAGE_NAME):$(IMAGE_TAG) --push .

build:
	docker build -t ghcr.io/deeeelin/$(IMAGE_NAME):$(IMAGE_TAG) --push .
deploy:
	kubectl apply -f $(KUBE_CONFIG)
	
clean:
	kubectl delete -f $(KUBE_CONFIG)

# Keep in mind , modelname cannot contain "/" 
test_local:
	curl -X POST http://dispatcher.default.127.0.0.1.sslip.io/ \
	-H "Content-Type: application/json" \
	-H "token: hi" \
	-H "model: llama" \
	-H "slo: 3" \
	-d '{"label": "original-service", "msg": "Hello, world!"}'

test_remote:
	curl -X POST http://localhost:8080 \
        -H "Host: dispatcher.default.127.0.0.1.nip.io" \
	    -H "Content-Type: application/json" \
        -H "model: infer" \
        -H "slo: 3" \
        -d '{"inputs":"What is Deep Learning?","parameters":{"max_new_tokens":20}}'

foward_remote:
	kubectl port-forward --namespace kourier-system $(kubectl get pod -n kourier-system -l "app=3scale-kourier-gateway" --output=jsonpath="{.items[0].metadata.name}") 8080:8080 19000:9000 8443:8443



 # ghcr.io/deeeelin/knative-service:latest