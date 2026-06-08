# VHAPE

This repository is a fork of the Kubernetes Autoscaler project focused on **VHAPE**, a custom Vertical Pod Autoscaler Recommender built on top of the upstream VPA Recommender codebase.

## Documentation

Start here:

- [VHAPE general guide](vertical-pod-autoscaler/vhape-docs/vhape-guide.md)  
  Overview of VHAPE, expected usage flow, `VhapePolicy`, the default heuristic, scaling rules, and operational notes.

- [Installation guide](vertical-pod-autoscaler/vhape-docs/install-guide.md)  
  How to install VHAPE, create a policy, create a VPA object, and check recommendations.

- [Development guide](vertical-pod-autoscaler/vhape-docs/dev-guide.md)  
  How to add new recommendation heuristics, new scaling rules, build the recommender image and push a helm chart.

## Upstream project

This codebase is based on the Kubernetes Autoscaler repository:

- [Kubernetes Autoscaler](https://github.com/kubernetes/autoscaler)
