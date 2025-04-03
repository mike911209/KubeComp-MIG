package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type MetricFetcher interface {
	FetchPodMetrics(pod v1.Pod) (map[Metric]float64, error)
}

func NewSimpleFetcher(config Config, kubeClient *kubernetes.Clientset) MetricFetcher {
	prometheusURL := os.Getenv("PROMETHEUS_URL")
	if prometheusURL == "" {
		prometheusURL = "http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090/api/v1/query"
	}

	return &SimpleFetcher{
		kubeClient:    kubeClient,
		prometheusURL: prometheusURL,
		namespace:     config.Namespace,
		cfgMapName:    config.cfgMapName,
	}
}

type SimpleFetcher struct {
	kubeClient    *kubernetes.Clientset
	prometheusURL string
	namespace     string
	cfgMapName    string
}

type Metric struct {
	Name            string  `yaml:"name"`
	Query           string  `yaml:"query"`
	SLO             float64 `yaml:"slo"`
	ScaleDownFactor float64 `yaml:"scaleDownFactor"`
	ScaleUpFactor   float64 `yaml:"scaleUpFactor"`
}

func (f *SimpleFetcher) FetchPodMetrics(pod v1.Pod) (map[Metric]float64, error) {
	// Fetch the config map for metrcs information
	configMaps, err := f.kubeClient.CoreV1().ConfigMaps(f.namespace).Get(context.TODO(), f.cfgMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get config map %s: %v", f.cfgMapName, err)
	}

	cfgName := strings.Split(pod.Labels["app"], "-")[0]
	if cfgName == "" {
		return nil, fmt.Errorf("failed to get app name from pod %s: %v", pod.Name, err)
	}
	metricCfg := configMaps.Data[cfgName]
	if metricCfg == "" {
		return nil, fmt.Errorf("failed to get metrics config for pod %s: %v", pod.Name, err)
	}

	var metricsInfo []Metric
	err = yaml.Unmarshal([]byte(metricCfg), &metricsInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %v", err)
	}

	// Fetch the metrics from Prometheus, store in a map
	// key: metric itself, value: metric value
	matricsMap := make(map[Metric]float64)
	for _, metric := range metricsInfo {
		query := metric.Query
		if query == "" {
			return nil, fmt.Errorf("query is empty for metric: %s", metric.Name)
		}
		// Replace the placeholder(%s) with the pod name
		count := strings.Count(query, "%s")
		args := make([]interface{}, count)
		for i := 0; i < count; i++ {
			args[i] = pod.Name
		}
		query = fmt.Sprintf(query, args...)

		// Make a request to Prometheus
		resp, err := http.Get(fmt.Sprintf("%s?query=%s", f.prometheusURL, query))
		if err != nil {
			return nil, fmt.Errorf("failed to fetch data from Prometheus: %v", err)
		}
		defer resp.Body.Close()

		// Parse the response
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %v", err)
		}
		var result struct {
			Status string `json:"status"`
			Data   struct {
				ResultType string `json:"resultType"`
				Result     []struct {
					Metric map[string]string `json:"metric"`
					Value  []interface{}     `json:"value"` // value[0] is timestamp, value[1] is the actual value
				} `json:"result"`
			} `json:"data"`
		}

		err = json.Unmarshal(body, &result)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON: %v", err)
		}
		for _, res := range result.Data.Result {
			value, err := strconv.ParseFloat(res.Value[1].(string), 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse value: %v", err)
			}
			matricsMap[metric] = value
		}
	}

	return matricsMap, nil
}
