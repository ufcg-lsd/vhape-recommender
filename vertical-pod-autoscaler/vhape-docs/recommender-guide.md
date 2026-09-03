# VHAPE Recommender

VHAPE is a custom Kubernetes Vertical Pod Autoscaler Recommender built on top of the v1.6.0 VPA Recommender codebase. It keeps the original VPA integration points, such as VPA objects, the VPA Admission Controller, and the VPA Updater, while replacing the core recommendation logic with a policy-driven architecture.

The central idea is that each VPA object can reference a `VhapePolicy`. The policy defines which heuristic should be used for CPU, which heuristic should be used for memory, and whether a scaling rule should constrain the final recommendation.

## Goals

VHAPE is designed to make recommender behavior easier to experiment with and extend. In particular, it aims to:

- make heuristics configurable through Kubernetes custom resources;
- allow CPU and memory to use different heuristics;
- allow estimators (heuristics) to configure their own sample storage strategy;
- support policy-level scaling constraints, such as blocking scale-up or blocking scale-down;
- preserve compatibility with the VPA API output format.

## How it works

A typical VHAPE setup includes:

- a workload, such as a `Deployment`;
- a running VHAPE Recommender instance;
- a `VhapePolicy` defining how CPU and memory recommendations are calculated through selected heuristics;
- a `VerticalPodAutoscaler` object targeting the workload and selecting both the VHAPE Recommender and the `VhapePolicy`.

The VPA object selects the VHAPE Recommender through `spec.recommenders[].name` and selects a `VhapePolicy` through the `vhape/policy` annotation.

The selected policy defines the heuristic configuration used for CPU and memory and can optionally apply a scaling rule to constrain the recommendation.

For policy configuration, heuristics, scaling rules, and recommendation constraints, see [VhapePolicy](vhape-policy.md).

## Setup guide

### 1. Install the VHAPE recommender

For installation instructions, see [VHAPE Recommender installation](recommender-installation.md).

### 2. Create or choose a `VhapePolicy`

The installation guide creates default `VhapePolicy` resources that can be used directly.

List available policies:

```bash
kubectl get vhapepolicies
```

This repository also provides an example at:

```text
vertical-pod-autoscaler/charts/vhape-recommender/templates/vhapepolicy-p93-percentile-hysteresis.yaml
```

Apply an additional policy with:

```bash
kubectl apply -f vhapepolicy.yaml
```

For policy configuration, heuristics, scaling rules, and recommendation constraints, see [VhapePolicy](vhape-policy.md).

### 3. Create a VPA object

The created VPA object must meet the following requirements:

- It must be created in the same namespace as the target workload.
- It must define spec.targetRef to identify the workload whose containers will receive resource recommendations.
- It must include the `autoscaling.vhape.io/recommender` label set to `vhape-recommender`.
- It must include the `vhape/policy` annotation, which selects a `VhapePolicy`. The annotation value must match the `VhapePolicy` name.
- The `spec.recommenders[].name` field must select the VHAPE Recommender `vhape-recommender`.

Optionally, set `updateMode: "Off"` to generate recommendations without applying them automatically.

This repository provides an example at:

```text
vertical-pod-autoscaler/pkg/recommender/yamls/vpa_object_example.yaml
```

Apply the VPA object:

```bash
kubectl apply -f vpa_object.yaml
```

### 4. Check recommendations

Describe the VPA:

```bash
kubectl describe vpa <vhape-vpa-object> -n <namespace>
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

## Operational notes

### Changes to the associated VHAPE policy restart estimators

If the `VhapePolicy` annotation on the VPA object changes after the estimators have been created, the existing estimators are recreated using the new policy configuration.

Because estimators retain container usage history, recreating them causes that history to be lost.

### Capping and post-processing

VHAPE follows the same general capping model used by the upstream VPA recommender, even though that model is split across different parts of the original VPA codebase.

In the upstream VPA recommender, recommendation bounds can come from multiple places:

| Source                                               | Scope                                              | Where it is applied                                                                                                                                                                                                            |
| ---------------------------------------------------- | -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `--pod-recommendation-min-cpu-millicores`            | Global minimum CPU recommendation per pod          | Applied during recommendation calculation. The pod-level value is divided by the number of containers and passed into the CPU estimators.                                                                                      |
| `--pod-recommendation-min-memory-mb`                 | Global minimum memory recommendation per pod       | Applied during recommendation calculation. The pod-level value is divided by the number of containers and passed into the memory estimators.                                                                                   |
| `--container-recommendation-max-allowed-cpu`         | Global maximum CPU recommendation per container    | Applied after recommendation generation by the VPA capping/post-processing logic.                                                                                                                                              |
| `--container-recommendation-max-allowed-memory`      | Global maximum memory recommendation per container | Applied after recommendation generation by the VPA capping/post-processing logic.                                                                                                                                              |
| `spec.resourcePolicy.containerPolicies[].minAllowed` | Per-VPA, per-container minimum                     | Applied after recommendation generation by the VPA capping/post-processing logic.                                                                                                                                              |
| `spec.resourcePolicy.containerPolicies[].maxAllowed` | Per-VPA, per-container maximum                     | Applied after recommendation generation by the VPA capping/post-processing logic. If both this and a global `--container-recommendation-max-allowed-*` value exist, the VPA-specific value takes precedence for that resource. |

The important detail is that the upstream VPA does not apply all caps in the same place. Pod-level minimum flags are part of the recommender configuration and affect the estimator output itself.

VHAPE currently preserves this split to stay compatible with the original VPA behavior while keeping the recommendation logic extensible. The recommender passes resource constraints to estimators, but each heuristic remains responsible for deciding how those constraints should affect its own recommendation. This leaves room for future heuristics to interpret and apply constraints according to their own semantics.

## Troubleshooting

### Recommendations stay equal to current requests

This can happen when the estimator does not have enough sample coverage yet.

The default `percentile-hysteresis` heuristic requires samples to span the configured `minimumCoverageWindow` before using percentile-based recommendations. Until then, it falls back to the current request or to the configured minimum.

It can also happen when a scaling rule is configured in the selected `VhapePolicy`:

```yaml
scalingRule: block-scale-up
```

or:

```yaml
scalingRule: block-scale-down
```

Check the VHAPE Recommender logs to identify why a recommendation is being constrained or falling back to the current request. The logs indicate when there is insufficient sample coverage and when a scaling rule is blocking scale-up or scale-down:

```bash
kubectl logs deployment/vhape-recommender -n kube-system
```
