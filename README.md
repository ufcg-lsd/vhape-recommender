# VHAPE

This repository is a fork of the Kubernetes Autoscaler project focused on **VHAPE**, a policy-driven extension of the Kubernetes Vertical Pod Autoscaler.

VHAPE includes:

* the **VHAPE Recommender**, which generates resource recommendations according to configurable `VhapePolicy` objects;
* the optional **VHAPE Watcher**, which automatically creates and maintains VPA objects for Deployments in configured namespaces.

## Documentation

### VHAPE Recommender

* [Recommender installation](vertical-pod-autoscaler/vhape-docs/recommender-installation.md)
  Install the VHAPE Recommender with Helm.

* [Recommender guide](vertical-pod-autoscaler/vhape-docs/recommender-guide.md)
  Configure workloads to use the VHAPE Recommender and inspect generated recommendations.

* [`VhapePolicy` reference](vertical-pod-autoscaler/vhape-docs/vhape-policy.md)
  Configure recommendation heuristics, resource-specific behavior, and scaling rules.

* [Recommender development](vertical-pod-autoscaler/vhape-docs/recommender-development.md)
  How to develop new recommendation heuristics and scaling rules.

### VHAPE Watcher

* [Watcher installation](vertical-pod-autoscaler/vhape-docs/watcher-installation.md)
  Install the VHAPE Watcher with Helm.

* [Watcher guide](vertical-pod-autoscaler/vhape-docs/watcher-guide.md)
  Configure watched namespaces, exclude workloads, and understand VPA reconciliation behavior.

### Common operations

* [Release guide](vertical-pod-autoscaler/vhape-docs/release-guide.md)
  Build container images and publish Helm charts.

* [Uninstall guide](vertical-pod-autoscaler/vhape-docs/uninstall-guide.md)
  Remove VHAPE components and related resources from a cluster.

## Upstream project

This codebase is based on the Kubernetes Autoscaler repository:

* [Kubernetes Autoscaler](https://github.com/kubernetes/autoscaler)
