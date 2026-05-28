# VHAPE

This repository is a fork of the Kubernetes Autoscaler project focused on **VHAPE**, a custom Vertical Pod Autoscaler Recommender built on top of the upstream VPA Recommender codebase.

## Documentation

Start here:

- [VHAPE general guide](vertical-pod-autoscaler/pkg/recommender/vhape-docs/vhape-guide.md)  
  Overview of VHAPE, expected usage flow, `VhapePolicy`, the default heuristic, scaling rules, and operational notes.

- [Installation guide](vertical-pod-autoscaler/pkg/recommender/vhape-docs/install-guide.md)  
  How to install VHAPE with the published image, configure RBAC, deploy the recommender, create a policy, create a VPA object, and check recommendations.

- [Development guide](vertical-pod-autoscaler/pkg/recommender/vhape-docs/dev-guide.md)  
  How to build the recommender image, add new recommendation heuristics, and add new scaling rules.

## Upstream project

This codebase is based on the Kubernetes Autoscaler repository:

- [Kubernetes Autoscaler](https://github.com/kubernetes/autoscaler)
