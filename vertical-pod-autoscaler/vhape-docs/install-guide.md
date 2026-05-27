# Installation guide

This guide explains how to install VHAPE and use it with a `VerticalPodAutoscaler` object.

VHAPE can be installed in two ways:

1. **Helm installation**  
   Planned, but not configured yet.

2. **Manual installation with the published image**  
   Available today. This uses the VHAPE Recommender image already published to Docker Hub and the YAML manifests provided in this repository.

Regardless of the installation method, using VHAPE for a workload follows the same basic steps:

1. install VHAPE on a Kubernetes cluster;
2. create or select a `VhapePolicy`;
3. create a `VerticalPodAutoscaler` object that selects the VHAPE Recommender, references the policy, and targets your workload;
4. check the generated recommendations.

## Prerequisites

You need:

- a Kubernetes cluster compatible with VPA version `v1.6.0`;
- VPA `v1.6.0` CRDs and core components installed on the cluster;
- `kubectl` configured for the cluster;
- Metrics Server or another metrics source available to the VPA recommender.

VHAPE is a custom recommender. It does not replace the VPA API itself. The cluster still needs the VPA CRDs and the VPA machinery that watches VPA objects and applies recommendations.

## Helm installation

Helm installation is planned but not configured yet.

## Manual installation with the published image

The manual installation uses the prebuilt VHAPE Recommender image:

```yaml
image: brunogb123/vhape-recommender-vhapev1:latest
```

The provided manifests at `vertical-pod-autoscaler/pkg/recommender/yamls` are already configured to use this image.

The manual installation flow is:

1. install the `VhapePolicy` CRD;
2. apply RBAC for the VHAPE Recommender;
3. deploy the VHAPE Recommender.

### 1. Install the `VhapePolicy` CRD

Apply the CRD manifest:

```bash
kubectl apply -f vhapepolicy-crd.yaml
```

Verify that the CRD was created:

```bash
kubectl get crd vhapepolicies.autoscaling.vhape.io
```

You should also be able to list `VhapePolicy` objects:

```bash
kubectl get vhapepolicies -A
```

At this point, the list may be empty. That is expected.

### 2. Apply RBAC for the VHAPE Recommender

The VHAPE Recommender runs with its own Kubernetes `ServiceAccount`:

```text
system:serviceaccount:kube-system:vhape-recommender
```

The RBAC manifest creates this `ServiceAccount` and grants the permissions required by the recommender.

These permissions include:

- reading pods, nodes, limit ranges, and workload targets;
- reading container metrics from the Kubernetes Metrics API;
- reading and patching VPA objects;
- reading, creating, updating, and deleting VPA checkpoints;
- reading `VhapePolicy` objects;
- using the leader-election lease `vhape-recommender-lease`.

Apply the RBAC manifest:

```bash
kubectl apply -f vhape-rbac.yaml
```

The provided manifests expect the VHAPE Recommender to run in `kube-system` using the `vhape-recommender` ServiceAccount.

If you intentionally deploy the recommender in a different namespace, update every `namespace: kube-system` reference in `vhape-rbac.yaml` so the `ServiceAccount`, `Role`, and `RoleBinding` are created in the same namespace as the recommender.

### 3. Deploy the VHAPE Recommender

Apply the recommender Deployment:

```bash
kubectl apply -f recommender_deployment.yaml
```

Check that the Pod is running:

```bash
kubectl get pods -n kube-system -l app=vhape-recommender
```

Check logs:

```bash
kubectl logs -n kube-system deploy/vhape-recommender
```

## Common usage flow

The following steps are common to manual installation and the future Helm installation.

After VHAPE is installed, you still need to:

1. create or select a `VhapePolicy`;
2. create or select a workload;
3. create a `VerticalPodAutoscaler` object that selects the VHAPE Recommender, references the policy, and targets your workload;
4. check recommendations.

### 1. Create or select a `VhapePolicy`

A `VhapePolicy` defines how VHAPE should calculate recommendations for a VPA object.

It configures:

- which heuristic should be used for CPU;
- which heuristic should be used for memory;
- whether an optional scaling rule should constrain the recommendation.

The repository provides a default policy at `vertical-pod-autoscaler/pkg/recommender/yamls/vhapepolicy-p93-default.yaml`. It uses the `percentile-hysteresis` heuristic for both CPU and memory, with no additional scaling rule enabled.

Apply the default policy:

```bash
kubectl apply -f vhapepolicy-p93-default.yaml
```

### 2. Create a VPA object

The example VPA object located at `vertical-pod-autoscaler/pkg/recommender/yamls/vpa_object.yaml` targets a Deployment named `my-app` in the `default` namespace.

Before applying it, update the `targetRef` to point to your workload:

```yaml
targetRef:
  apiVersion: apps/v1
  kind: Deployment
  name: my-app
```

The VPA object must also select the VHAPE Recommender and reference a `VhapePolicy`:

```yaml
metadata:
  annotations:
    vhape/policy: "kube-system/vhape-policy-p93-default"
spec:
  recommenders:
    - name: vhape-recommender
```

The `vhape/policy` annotation selects the `VhapePolicy`.

The `recommenders` field selects the VHAPE Recommender instance. The value of `spec.recommenders[].name` must match the `--recommender-name` configured in the recommender Deployment.

After checking the workload target, recommender name, and policy annotation, apply the VPA object:

```bash
kubectl apply -f vpa_object.yaml
```

### 3. Check recommendations

Describe the VPA:

```bash
kubectl describe vpa vhape-vpa-object -n default
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
