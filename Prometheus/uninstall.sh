#!/bin/bash
# kubectl delete -f prometheus-volume.yaml
# kubectl delete -f grafana-volume.yaml
helm uninstall -n monitoring prometheus
