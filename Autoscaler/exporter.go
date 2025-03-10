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
	gpuResource    *prometheus.GaugeVec
	eventsChan     chan Event
	updateInterval time.Duration
}

type Event struct {
	revisionName string
	scalingType  ScaleDecision // NotScaling, ScalingDown, ...
	resourceName string
}

// NewExporter creates a new Exporter instance
func NewExporter(updateInterval time.Duration) *Exporter {
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
		gpuResource:    gpuResource, // TODO: maybe change to another name
		eventsChan:     make(chan Event),
		updateInterval: updateInterval,
		// TODO: add more metric
	}
}

// StartExporter starts the Prometheus exporter
func (e *Exporter) StartExporter() {
	go func() {
		// Start the exporter
		log.Printf("Starting exporter...")
		e.gpuResource.Reset()
		go func() {
			ticker := time.NewTicker(e.updateInterval)
			defer ticker.Stop()
			for {
				<-ticker.C
				switch event := <-e.eventsChan; event.scalingType {
				case NotScaling, ScalingOut:
					e.gpuResource.With(prometheus.Labels{"revision": event.revisionName}).Set(e.resourceToCPU(event.resourceName))
				case ScalingIn, ScalingUp, ScalingDown:
					flag := e.gpuResource.Delete(prometheus.Labels{"revision": event.revisionName})
					if !flag {
						log.Printf("Error deleting metric")
					}
				default:
					log.Printf("Error: unknown scaling type")
				}
			}
		}()

		// Serve Prometheus metrics
		http.Handle("/metrics", promhttp.Handler())
		log.Fatal(http.ListenAndServe(":8080", nil))
	}()
}

// TODO: change implementation (maybe use regex), and use a better function name
func (e *Exporter) resourceToCPU(resource string) float64 {
	if resource == "" {
		return 0
	}
	cpu, _ := strconv.ParseFloat(resource[15:16], 64)
	return cpu
}

func (e *Exporter) SendScalingEvent(revisionData RevisionData, scalingType ScaleDecision) {
	e.eventsChan <- Event{
		revisionName: revisionData.name,
		resourceName: revisionData.gpuName,
		scalingType:  scalingType,
	}
}
