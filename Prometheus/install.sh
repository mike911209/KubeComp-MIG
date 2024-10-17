#!/bin/bash
kubectl apply -f prometheus-volume.yaml
kubectl apply -f grafana-volume.yaml
helm install prometheus prometheus-community/kube-prometheus-stack -f custom-prometheus-values.yaml -n monitoring --create-namespace

