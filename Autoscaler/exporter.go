package main

import (
	"log"
	"net/http"
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
	revisionData RevisionData
	scalingType  ScaleDecision // NotScaling, ScalingDown, ...
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
					e.gpuResource.With(prometheus.Labels{"revision": event.revisionData.name}).Set(event.revisionData.gpuResource.cpuSize)
				case ScalingIn, ScalingUp, ScalingDown:
					flag := e.gpuResource.Delete(prometheus.Labels{"revision": event.revisionData.name})
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

func (e *Exporter) SendScalingEvent(revisionData RevisionData, scalingType ScaleDecision) {
	e.eventsChan <- Event{
		revisionData: revisionData,
		scalingType:  scalingType,
	}
}
