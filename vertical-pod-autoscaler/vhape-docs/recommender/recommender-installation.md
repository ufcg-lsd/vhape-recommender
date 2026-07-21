# Installation guide

This guide explains how to install VHAPE and use it with a `VerticalPodAutoscaler` object.

VHAPE is installed with Helm. The chart installs the `VhapePolicy` CRD, RBAC, the VHAPE Recommender Deployment, and the configured `VhapePolicy` objects.

The basic flow is:

1. install the VPA components;
2. install VHAPE;
3. create or choose a `VhapePolicy`;
4. create a `VerticalPodAutoscaler` object that selects the VHAPE Recommender, references the chosen policy, and targets your workload;
5. check the generated recommendations.

## Prerequisites

You need:

- a Kubernetes cluster running version 1.28.0 or later;
- kubectl configured to access the cluster;
- Helm installed locally;
- Metrics Server installed and accessible to the VPA recommender.

## Install upstream VPA components

Before installing VHAPE, install the v1.6.0 Kubernetes Vertical Pod Autoscaler chart with the default recommender disabled.

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

With this setup, VHAPE can still generate recommendations, while the cluster avoids running the upstream VPA components that can apply them automatically. However, for most inspection-only use cases, prefer the standard and more flexible installation: keep the updater and admission controller enabled, and set updateMode: "Off" on the VPA object, as described in [Create a VPA object](#2-create-a-vpa-object).

Verify the installation:

```bash
kubectl get crd verticalpodautoscalers.autoscaling.k8s.io
kubectl get pods -n kube-system
```

## VHAPE Helm installation

The Helm installation installs:

- the `VhapePolicy` CRD;
- RBAC for the VHAPE Recommender;
- the VHAPE Recommender Deployment;
- Some `VhapePolicy` objects configured with default values.

Install VHAPE from the published OCI chart:

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

## Usage flow

After VHAPE is installed with Helm, you still need to:

1. create or select a `VhapePolicy`;
2. create a `VerticalPodAutoscaler` object that selects the VHAPE Recommender, references the policy, and targets a workload;
3. check recommendations.

### 1. Create or select a `VhapePolicy`

A `VhapePolicy` defines how VHAPE should calculate recommendations for a VPA object.

It configures:

- which heuristic should be used for CPU;
- which heuristic should be used for memory;
- whether an optional scaling rule should constrain the recommendation.

The Helm chart may create `VhapePolicy` objects from the configured chart values.

You can list the available policies with:

```bash
kubectl get vhapepolicies -A
```

To define an additional policy, create a new `VhapePolicy` manifest and apply it to the cluster.

The repository provides a default policy at `vertical-pod-autoscaler/pkg/recommender/yamls/vhapepolicy-p93-default.yaml`. It uses the `percentile-hysteresis` heuristic for both CPU and memory, with no additional scaling rule enabled.

Edit the file as needed, then apply it with:

```bash
kubectl apply -f vhapepolicy-p93-default.yaml
```

### 2. Create a VPA object

The example VPA object located at `vertical-pod-autoscaler/pkg/recommender/yamls/vpa_object.yaml` targets a Deployment named `my-app` in the `default` namespace.

Before applying it, update the VPA object so that it points to your workload. The VPA object must be created in the same namespace as the target Deployment. For example, if your Deployment is in the `default` namespace, the VPA object must also be in the `default` namespace.

```yaml
metadata:
  namespace: default
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: my-app
```

Then configure the VPA object to use the VHAPE Recommender and reference the desired `VhapePolicy`:

```yaml
metadata:
  annotations:
    vhape/policy: "kube-system/vhape-policy-p93-default"
spec:
  recommenders:
    - name: vhape-recommender
```

The `vhape/policy` annotation selects the `VhapePolicy`.

The `spec.recommenders[].name` field selects the VHAPE Recommender instance. Its value must match the `--recommender-name` configured in the recommender Deployment.

To generate recommendations without applying them automatically, set the VPA update mode to `Off`:

```yaml
spec:
  updatePolicy:
    updateMode: "Off"
```

After checking the VPA namespace, workload target, recommender name, policy annotation, and update mode, apply the VPA object:

```bash
kubectl apply -f vpa_object.yaml
```

### 3. Check recommendations

Describe the VPA:

```bash
kubectl describe vpa <vhape-vpa-object> -n default
```

Recommendations should eventually appear under:

```yaml
status:
  recommendation:
    containerRecommendations:
      - containerName: app
        target: ...
        lowerBound: ...
        upperBound: ...
        uncappedTarget: ...
```

## Troubleshooting

### Recommendations stay equal to current requests

This can happen when the estimator does not have enough sample coverage yet.

The default `percentile-hysteresis` heuristic requires samples to cover a minimum fraction of the configured sliding window before using percentile-based recommendations. Until then, it falls back to the current request or to the configured minimum.

It can also happen when a scaling rule is configured in the selected `VhapePolicy`:

```yaml
scalingRule: block-scale-up
```

or:

```yaml
scalingRule: block-scale-down
```

### Policy changes do not appear to take effect

Estimators are cached by VPA namespace/name. This means their sample history persists across recommender loops for the same VPA.

If a `VhapePolicy` is changed after estimators have already been created, the existing estimators may continue using the old configuration until the recommender restarts.

Restart the recommender Deployment:

```bash
kubectl rollout restart deployment/vhape-recommender -n kube-system
```

## Uninstall

### 1. Delete existing VPA objects

List all VPA objects in the cluster:

```bash
kubectl get vpa -A
```

Delete the VPA objects that were using VHAPE:

```bash
kubectl delete vpa <vpa-name> -n <namespace>
```

To delete **all** VPA objects in the cluster (including these not managed by VHAPE!):

```bash
kubectl delete verticalpodautoscalers.autoscaling.k8s.io --all -A
```

### 2. Delete existing VhapePolicy objects

List all VHAPE policies:

```bash
kubectl get vhapepolicies -A
```

Delete all VHAPE policies:

```bash
kubectl delete vhapepolicies.autoscaling.vhape.io --all -A
```

### 3. Uninstall VHAPE

Uninstall the VHAPE Helm release:

```bash
helm uninstall vhape-recommender -n kube-system
```

### 4. Delete the VHAPE CRD

```bash
kubectl delete crd vhapepolicies.autoscaling.vhape.io
```
