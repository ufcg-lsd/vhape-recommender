# Vhape Policy

A `VhapePolicy` defines how the VHAPE Recommender calculates CPU and memory recommendations for a VPA.

Each policy configures one heuristic for CPU and one heuristic for memory. CPU and memory can use different heuristic configurations.

A policy can also define an optional scaling rule that constrains the recommendation after it is calculated.

This repository provides an example at:

```text
vertical-pod-autoscaler/pkg/recommender/yamls/vhapepolicy-p93-default.yaml
```

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
        headroom: 0.05
        slidingWindow: 24h
  scalingRule: ""
```

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
    memory:
      percentile-hysteresis:
        percentile: 0.95
        headroom: 0.20
        slidingWindow: 12h
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
