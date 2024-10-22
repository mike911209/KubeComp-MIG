package main

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Exporter struct to encapsulate Prometheus metrics
type Exporter struct {
	gpuResource *prometheus.GaugeVec
}

// NewExporter creates a new Exporter instance
func NewExporter() *Exporter {
	gpuResource := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "KubeComp_gpu_resource",
			Help: "Mig slice used by KubeComp pod",
		},
		[]string{"revision"},
	)

	// Register the metrics with Prometheus
	prometheus.MustRegister(gpuResource)

	return &Exporter{
		gpuResource: gpuResource,
	}
}

// StartExporter starts the Prometheus exporter
func (e *Exporter) StartExporter(interval time.Duration) {
	// Start the exporter
	log.Printf("Starting exporter...")
	e.gpuResource.Reset()
	go func() {
		for {
			event := <-EventsChan
			if event.scalingType == SCALING_IN {
				_, err := e.gpuResource.GetMetricWith(prometheus.Labels{"revision": event.revisionName})
				// check the metric does exist before deleting it
				if err == nil {
					e.gpuResource.Delete(prometheus.Labels{"revision": event.revisionName})
				}
				continue
			}
			err := e.updateMetrics(event)
			if err != nil {
				log.Printf("Error updating metrics: %v", err)
			}
			time.Sleep(interval)
		}
	}()

	// Serve Prometheus metrics
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Update metrics by querying the Kubernetes API
func (e *Exporter) updateMetrics(event Event) error {
	// Update the gauge
	e.gpuResource.With(prometheus.Labels{"revision": event.revisionName}).Set(e.resourceToCPU(event.resourceName))
	return nil
}

func (e *Exporter) resourceToCPU(resource string) float64 {
	if resource == "" {
		return 0
	}
	cpu, _ := strconv.ParseFloat(resource[15:16], 64)
	return cpu
}
