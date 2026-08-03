# Uninstalling VHAPE components

This guide explains how to uninstall VHAPE and clean its components completely.

If both the VHAPE Watcher and VHAPE Recommender are installed, uninstall the watcher first so it stops managing VPA objects before the recommender is removed.

## VHAPE Watcher

Uninstall the Helm release:

```bash
helm uninstall vhape-watcher -n kube-system
```

To remove the watcher CRDs as well:

```bash
kubectl delete crd vhapewatchednamespaces.autoscaling.vhape.io
kubectl delete crd vhapeignoredworkloads.autoscaling.vhape.io
```

## VHAPE Recommender

Uninstall the Helm release:

```bash
helm uninstall vhape-recommender -n kube-system
```

To remove the `VhapePolicy` CRD as well:

```bash
kubectl delete crd vhapepolicies.autoscaling.vhape.io
```

## Remove leftover `VPA` objects

Check for VPA objects labeled as using a VHAPE Recommender:

```bash
kubectl get vpa -A -l autoscaling.vhape.io/recommender
```

The selector checks for the existence of the `autoscaling.vhape.io/recommender` label.

VPA objects associated with VHAPE are expected to include this label. The VHAPE Watcher adds it automatically to generated VPAs, and the VPA examples provided in the recommender guide and in the repository include it as well. Manually created VPAs may be missing the label if it was not added during configuration.

To delete the labeled VPA objects:

```bash
kubectl delete vpa -A -l autoscaling.vhape.io/recommender
```

## Upstream VPA

Uninstall the upstream VPA only when it is no longer required by other workloads or components in the cluster:

```bash
helm uninstall vertical-pod-autoscaler -n kube-system
```
