# VHAPE Recommender general guide

VHAPE is a custom Kubernetes Vertical Pod Autoscaler Recommender built on top of the upstream VPA Recommender codebase. It keeps the original VPA integration points, such as VPA objects, the VPA Admission Controller, and the VPA Updater, while replacing the core recommendation logic with a policy-driven architecture.

The central idea is that each VPA object can reference a `VhapePolicy`. The policy defines which heuristic should be used for CPU, which heuristic should be used for memory, and whether a scaling rule should constrain the final recommendation.

## Goals

VHAPE is designed to make recommender behavior easier to experiment with and extend. In particular, it aims to:

* make heuristics configurable through Kubernetes custom resources;
* allow CPU and memory to use different estimators;
* allow estimators to configure their own sample storage strategy;
* support policy-level scaling constraints, such as blocking scale-up or blocking scale-down;
* preserve compatibility with the VPA API output format.

## Expected simplified usage flow

A typical VHAPE setup has four main components:

1. a workload, such as a `Deployment`;
2. a running VHAPE Recommender instance registered with a recommender name, such as `vhape-recommender`;
3. a `VerticalPodAutoscaler` object targeting the workload, selecting the VHAPE Recommender and a VHAPE Policy;
4. a `VhapePolicy` object defining how recommendations should be calculated.

For example, a VPA object can reference the default policy like this:

```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: my-app-vpa
  namespace: default
  annotations:
    vhape/policy: "kube-system/vhape-policy-p93-default"
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: my-app
  recommenders:
    - name: vhape-recommender
```

Then the referenced `VhapePolicy` defines the recommendation behavior:

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapePolicy
metadata:
  name: vhape-policy-p93-default
  namespace: kube-system
spec:
  resources:
    cpu:
      percentile-hysteresis:
        percentile: 0.93
        headroom: 0.10
        slidingWindow: 24h
    memory:
      percentile-hysteresis:
        percentile: 0.93
        headroom: 0.10
        slidingWindow: 24h
  scalingRule: ""
```

## Installation and usage

See the detailed installation guide:

[Installation guide](install-guide.md)

## VhapePolicy CRD

### `spec.resources`

`spec.resources` must define both `cpu` and `memory`.

Each resource must define exactly one heuristic. Today, the only supported heuristic is `percentile-hysteresis`.

### `spec.scalingRule`

`spec.scalingRule` is optional. Valid values are:

| Value              | Meaning                                                                                                         |
| ------------------ | --------------------------------------------------------------------------------------------------------------- |
| `""`               | No scaling rule. The estimator recommendation is used as-is, subject to normal constraints and post-processors. |
| `block-scale-up`   | Prevents the final visible recommendation from going above the current request.                                 |
| `block-scale-down` | Prevents the final visible recommendation from going below the current request.                                 |

## Percentile hysteresis heuristic

`percentile-hysteresis` is the default VHAPE heuristic.

It recommends resources from recent usage samples using this formula:

```text
base = percentile(recent samples)
uncappedTarget = ceil(base * (1 + headroom))
target = clamp(uncappedTarget, min, max)
lowerBound = ceil(target * 0.9)
upperBound = ceil(target * 1.1)
```

The `percentile-hysteresis` estimator stores samples per container. Samples are timestamped when they are fed to the estimator. On each feed or recommendation cycle, samples older than the configured sliding window are removed.

### Configuration

```yaml
percentile-hysteresis:
  percentile: 0.93
  headroom: 0.10
  slidingWindow: 24h
```

| Field           | Type                       | Meaning                                                                             |
| --------------- | -------------------------- | ----------------------------------------------------------------------------------- |
| `percentile`    | number between `0` and `1` | Usage percentile used as the base recommendation. `0.93` means the 93rd percentile. |
| `headroom`      | number `>= 0`              | Extra capacity added on top of the selected percentile. `0.10` means 10%.           |
| `slidingWindow` | duration string            | How long samples remain eligible. Examples: `30m`, `2h`, `24h`.                     |

### Minimum window coverage

The estimator requires samples to cover a minimum fraction of the sliding window before producing a percentile-based recommendation.

The current default is:

```text
minWindowCoverageRatio = 0.02
```

For a `24h` sliding window, this means samples must span at least about 29 minutes.

If there is not enough coverage, the estimator falls back to:

1. `CurrentRequest`, when it is greater than zero;
2. `Min`, when the current request is missing or zero.

This avoids making aggressive recommendations from too little data.

## Operational notes

### Estimator cache lifecycle

Estimators are cached by VPA namespace/name. This means their sample history persists across recommender loops for the same VPA.

If a `VhapePolicy` is changed after estimators have already been created, the existing estimators may continue using the old configuration until the recommender restarts.

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