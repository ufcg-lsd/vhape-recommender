# Vhape Policy

A `VhapePolicy` defines how the VHAPE Recommender calculates CPU and memory recommendations for a VPA.

`VhapePolicy` is a cluster-scoped resource. A policy is identified only by its name, which must be unique across the cluster.

Each policy configures one heuristic for CPU and one heuristic for memory. CPU and memory can use different heuristic configurations.

A policy can also define an optional scaling rule that constrains the recommendation after it is calculated.

This repository provides an example at:

```text
vertical-pod-autoscaler/charts/vhape-recommender/templates/vhapepolicy-p93-percentile-hysteresis.yaml
```

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapePolicy
metadata:
  name: p93-percentile-hysteresis
spec:
  resources:
    cpu:
      percentile-hysteresis:
        percentile: 0.93
        headroom: 0.10
        slidingWindow: 24h
        minimumCoverageWindow: 30m
    memory:
      percentile-hysteresis:
        percentile: 0.93
        headroom: 0.05
        slidingWindow: 24h
        minimumCoverageWindow: 30m
  scalingRule: ""
```

`VhapePolicy.spec` is immutable. After a policy is created, its CPU heuristic, memory heuristic, parameters, and scaling rule cannot be edited in place. To change policy behavior, create another `VhapePolicy`.


## Resource heuristics

`spec.resources` must define both `cpu` and `memory`.

Each resource (`cpu` and `memory`) must define exactly one heuristic. Currently, the supported heuristic is [`percentile-hysteresis`](#percentile-hysteresis).

Example:

```yaml
spec:
  resources:
    cpu:
      percentile-hysteresis:
        percentile: 0.93
        headroom: 0.10
        slidingWindow: 24h
        minimumCoverageWindow: 30m
    memory:
      percentile-hysteresis:
        percentile: 0.95
        headroom: 0.20
        slidingWindow: 12h
        minimumCoverageWindow: 30m
```

## Scaling rules

`spec.scalingRule` is optional. Valid values are:

| Value              | Meaning                                                                                                         |
| ------------------ | --------------------------------------------------------------------------------------------------------------- |
| `""`               | No scaling rule. The estimator recommendation is used as-is, subject to normal constraints and post-processors. |
| `block-scale-up`   | Prevents the final visible recommendation from going above the current request.                                 |
| `block-scale-down` | Prevents the final visible recommendation from going below the current request.                                 |

Example:

```yaml
spec:
  scalingRule: block-scale-up
```

## Percentile hysteresis

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
  minimumCoverageWindow: 30m
```

| Field                   | Type                       | Meaning                                                                                                  |
| ----------------------- | -------------------------- | -------------------------------------------------------------------------------------------------------- |
| `percentile`            | number between `0` and `1` | Usage percentile used as the base recommendation. `0.93` means the 93rd percentile.                      |
| `headroom`              | number `>= 0`              | Extra capacity added on top of the selected percentile. `0.10` means 10%.                                |
| `slidingWindow`         | duration string            | How long samples remain eligible. Examples: `30m`, `2h`, `24h`.                                          |
| `minimumCoverageWindow` | duration string            | Minimum time span between the oldest and newest samples. It must be less than `slidingWindow`.           |

### Minimum window coverage

The estimator requires samples to span at least `minimumCoverageWindow` before producing a percentile-based recommendation. This is an absolute duration and is independent of the size of `slidingWindow`.

For example, with `minimumCoverageWindow: 30m`, the time between the oldest and newest eligible samples must be at least 30 minutes. The configuration is rejected when `minimumCoverageWindow` is equal to or greater than `slidingWindow`.

If there is not enough coverage, the estimator falls back to:

1. `CurrentRequest`, when it is greater than zero;
2. The default minimum value configured through VPA global flags, when the current request is missing.

This avoids making aggressive recommendations from too little data.
