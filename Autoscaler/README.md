# K-SISS Autoscaler

K-SISS Autoscaler is an GPU-aware autoscaler for inference workloads (e.g., LLMs) deployed in Knative and Kubernetes.  
It adjusts GPU resource tiers (MIG, MPS, or full GPU) dynamically based on application metrics like latency and throughput.

## Features

- **Dynamic GPU Tier Scaling** — supports MIG, MPS, and full GPU
- **Metric-Based Decisions** — driven by Prometheus metrics (e.g., latency, tokens/sec)
- **Knative + Prometheus Integration** — integrated with Knatie and Prometheus

## Build & Run
> Make sure to configure the `makefile` for your Docker or GitHub container registry.

Build your image:
```
make build
```

Edit configuration.yaml to match your environment (e.g., namespace, metric queries).

Deploy into you k8s cluster:
```
make deploy
```
>  This will use configuration.yaml to deploy the autoscaler

## Project Structure
```
Autoscaler/
├── autoscaler.go
├── configuration.yaml
├── Dockerfile
├── exporter.go
├── go.mod
├── go.sum
├── gpuRegistry.go
├── gpuResource.go
├── knativeHelper.go
├── makefile
├── metricsFetcher.go
├── README.md
└── scaler.go
``` 

### autoscaler.go
Top-level module that processes each inference service and initializes all submodules.

### exporter.go
Promehteus metrics exporter, enabling visualization of scaling activity.

Exposes the following metrics:
- Gpu resource currently used by inference services
- Inference performance metrics obtain from Prometheus

### gpuRegistry.go
GPU resource manager, Maintains available GPU tiers and provides tier resolution logic.

Handles:
- Cluster GPU tier initialization
- Validating scaling actions based on available tiers

### gpuResource.go
Defines the `gpuResource` struct, which encapsulates all metadata about a GPU resource (type, tier, CPU/memory size).

### knativeHelper.go
Utility functions to interact with Knative Services and Revisions.

### metricsFetcher.go
Fetches Prometheus metrics based on queries defined in `configuration.yaml`.
- Reading metric configurations from the ConfigMap
- Querying metrics per pod to inform scaling logic

### scaler.go
Contains the scaling policy and decision logic.
- Determines whether to scale up, down, in, or out
- Does not track GPU availability — it only decides what should happen
- Executes scaling by:
    - Creating new inference pods with upgraded resources
    - Updating Knative traffic routing
    - Deleting old revisions (in case of up/down/in scaling)

### configuration.yaml
Defines all Kubernetes resources needed to run the autoscaler. Edit this file to configure namespaces, metric queries.

## Customization
### Add new metrics for scaling decision
To add new metrics for scaling decisions, modify the autoscaler-config ConfigMap (defined in `configuration.yaml`):

For example, say you want to create metrics for app llama3:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: autoscaler-config
  namespace: default
  labels:
    app: autoscaler
data:
  llama3: |
    - name: <define your metric name>
      query: <write your promQL query here>
      slo: <slo to scale inference server>
      scaleDownFactor:
      scaleUpFactor:
```
> Each section under a key like llama3 corresponds to one inference service.

### Custom scaling policy
To change how scaling decisions are made, modify the `DecideScale` function in `scaler.go`.

> The current implementation only considers a single metric when deciding to scale up or down. You can extend it to support multi-metric logic or weighted scoring.

### Register new gpu resources
Extend gpuRegistry.go to define new GPU tiers, such as:
- MIG configuration
- MPS virtual slices

## Author
Mike Li (Bin-Lun Li)