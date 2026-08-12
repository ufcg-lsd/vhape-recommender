# VHAPE Watcher installation

This guide explains how to install the VHAPE Watcher with Helm.

## Prerequisites

You need:

* a Kubernetes cluster running version 1.28.0 or later;
* `kubectl` configured to access the cluster;
* Helm installed locally;
* the VHAPE Recommender installed.

## Install the VHAPE Watcher

The Helm chart installs:

* the `VhapeWatchedNamespace` CRD;
* the `VhapeWatchedNamespaceRegex` CRD;
* the `VhapeIgnoredNamespace` CRD;
* the `VhapeIgnoredWorkload` CRD;
* RBAC for the VHAPE Watcher;
* the VHAPE Watcher Deployment.

By default, the Helm chart also creates a `VhapeIgnoredNamespace` resource for `kube-system`, excluding workloads in that namespace from automatic watcher management.

From the repository root, install the chart:

```bash
helm upgrade --install vhape-watcher \
  oci://registry-1.docker.io/vtexlsd/vhape-watcher-chart \
  --namespace kube-system \
  --wait
```

Check that the watcher Pod is running:

```bash
kubectl get pods -n kube-system -l app=vhape-watcher
```

Check logs:

```bash
kubectl logs -n kube-system deploy/vhape-watcher
```

## Upgrading watcher CRDs

Helm installs files from a chart's `crds/` directory on the first installation, but it does not upgrade those CRDs on later releases. Apply the current CRD explicitly if you're upgrading your current release:

```bash
kubectl apply -f vertical-pod-autoscaler/charts/vhape-watcher/crds/vhapewatchednamespace-crd.yaml
kubectl apply -f vertical-pod-autoscaler/charts/vhape-watcher/crds/vhapewatchednamespaceregex-crd.yaml
kubectl apply -f vertical-pod-autoscaler/charts/vhape-watcher/crds/vhapeignorednamespace-crd.yaml
kubectl apply -f vertical-pod-autoscaler/charts/vhape-watcher/crds/vhapeignoredworkload-crd.yaml
```

## Next steps

See the [VHAPE Watcher guide](watcher-guide.md) to configure namespace selection, regex-based defaults, and exceptions.
