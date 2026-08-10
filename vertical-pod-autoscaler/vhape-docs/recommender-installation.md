# VHAPE Recommender installation

This guide explains how to install the VHAPE Recommender with Helm.

## Prerequisites

You need:

- a Kubernetes cluster running version 1.28.0 or later;
- `kubectl` configured to access the cluster;
- Helm installed locally;
- Metrics Server installed and accessible to the VPA recommender.

## Install upstream VPA components

Before installing the VHAPE Recommender, install the v1.6.0 Kubernetes Vertical Pod Autoscaler chart with the default recommender disabled.

```bash
helm repo add autoscalers https://kubernetes.github.io/autoscaler
helm repo update
helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler \
  --namespace kube-system \
  --create-namespace \
  --set recommender.enabled=false \
  --wait
```

If you strictly do not want the cluster to run components capable of applying recommendations, you can also disable the updater and admission controller:

```bash
helm repo add autoscalers https://kubernetes.github.io/autoscaler
helm repo update
helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler \
  --namespace kube-system \
  --create-namespace \
  --set recommender.enabled=false \
  --set updater.enabled=false \
  --set admissionController.enabled=false \
  --wait
```

With this setup, the VHAPE Recommender can still generate recommendations, while the cluster avoids running the upstream VPA components that can apply them automatically. However, for most inspection-only use cases, prefer the standard and more flexible installation: keep the updater and admission controller enabled, and set `updateMode: "Off"` on the VPA object.

Verify the VPA installation:

```bash
kubectl get crd verticalpodautoscalers.autoscaling.k8s.io
kubectl get pods -n kube-system
```

## Install the VHAPE Recommender

The Helm chart installs:

- the `VhapePolicy` CRD;
- RBAC for the VHAPE Recommender;
- the VHAPE Recommender Deployment;
- `VhapePolicy` objects configured through the chart values.

Install the published OCI chart:

```bash
helm upgrade --install vhape-recommender \
  oci://registry-1.docker.io/vtexlsd/vhape-recommender-chart \
  --namespace kube-system \
  --wait
```

Check that the recommender Pod is running:

```bash
kubectl get pods -n kube-system -l app=vhape-recommender
```

Check logs:

```bash
kubectl logs -n kube-system deploy/vhape-recommender
```

## Upgrading the chart and CRD

Helm installs files from a chart's `crds/` directory on the first installation, but it does not upgrade those CRDs on later releases. Apply the current CRD explicitly if you're upgrading your current release:

```bash
kubectl apply -f vertical-pod-autoscaler/charts/vhape-recommender/crds/vhapepolicy-crd.yaml
```

## Next steps

See the [VHAPE Recommender guide](recommender-guide.md) to setup the VHAPE recommender and inspect recommendations.
