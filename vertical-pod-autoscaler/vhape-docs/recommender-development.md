# VHAPE Recommender development

This guide explains how to extend the VHAPE Recommender with new recommendation heuristics and scaling rules.

A heuristic is responsible for producing resource recommendations from usage data. A scaling rule is applied later, after the heuristic runs, to optionally constrain how the recommendation moves relative to the current request.

For image builds and Helm chart publishing, see the [release guide](release-guide.md).

## Adding a new heuristic

A heuristic is self-contained in a single file under `logic/estimators`, holding three things:

1. the estimator, which performs the runtime recommendation logic;
2. a config struct, which decodes the parameters written in the `VhapePolicy`;
3. an `init` function registering it, so no shared file needs to be edited.

### Step 1. Create the estimator

Create a new file in `logic/estimators`, for example:

```text
logic/estimators/my_heuristic.go
```

Implement `ResourceEstimator`:

```go
type ResourceEstimator interface {
	FeedSamples(containerName string, samples []model.ResourceAmount)
	GetSingleResourceRecommendation(
		containerName string,
		constraints ContainerResourceConstraints,
	) recommendation.SingleResourceRecommendation
}
```

The estimator is responsible for deciding:

- how it stores samples;
- how it handles missing data;
- how it calculates `Target`, `LowerBound`, `UpperBound`, and `UncappedTarget`;
- how it applies `constraints.Min`, `constraints.Max`, and `constraints.CurrentRequest`, when relevant.

### Step 2. Create the config struct

In the same file, add a struct with the parameters as they appear in the `VhapePolicy`, using JSON tags. It must implement `HeuristicSpec`:

```go
type HeuristicSpec interface {
	NewEstimator(resourceName model.ResourceName) ResourceEstimator
}
```

For example:

```go
// MyHeuristic is the name used to select this heuristic in a VhapePolicy.
// This string is the YAML key used under spec.resources.cpu and spec.resources.memory.
const MyHeuristic = "my-heuristic"

type MyHeuristicSpec struct {
	SomeParameter float64         `json:"someParameter"`
	SomeWindow    metav1.Duration `json:"someWindow"`
}

func (s *MyHeuristicSpec) NewEstimator(resourceName model.ResourceName) ResourceEstimator {
	return NewMyHeuristicEstimator(resourceName, s.SomeParameter, s.SomeWindow.Duration)
}
```

Use `metav1.Duration` for durations: it decodes strings such as `24h` directly.

### Step 3. Register the heuristic

Still in the same file, register it from an `init` function. This is the only wiring needed — there is no central switch to update:

```go
func init() {
	RegisterHeuristic(MyHeuristic, func(config []byte) (HeuristicSpec, error) {
		spec := &MyHeuristicSpec{}
		if err := json.Unmarshal(config, spec); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		return spec, nil
	})
}
```

`BuildHeuristic` in `logic/estimators/heuristics.go` looks the name up in the registry and calls this factory. Registering the same name twice panics at startup.

### Step 4. Update the CRD schema

Update `charts/vhape-recommender/crds/vhapepolicy-crd.yaml` so Kubernetes accepts the new heuristic. This step is not optional: the schema is structural, so parameters the CRD does not declare are pruned by the API server before the recommender sees them, and the policy then fails with `must define exactly one heuristic`.

Example:

```yaml
my-heuristic:
  type: object
  required:
    - someParameter
  properties:
    someParameter:
      type: number
      minimum: 0
```

### Step 5. Create a policy using the new heuristic

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapePolicy
metadata:
  name: my-heuristic-policy
spec:
  resources:
    cpu:
      my-heuristic:
        someParameter: 1.0
    memory:
      percentile-hysteresis:
        percentile: 0.93
        headroom: 0.10
        slidingWindow: 24h
        minimumCoverageWindow: 30m
  scalingRule: ""
```

## Adding a new scaling rule

A scaling rule transforms one `SingleResourceRecommendation` after the estimator runs and before CPU and memory are combined into a full container recommendation.

### Step 1. Create the rule

Create a new file in `logic/scalingrules`, for example:

```text
logic/scalingrules/my_rule.go
```

Implement `ScalingRule`:

```go
type ScalingRule interface {
	Apply(
		resourceRecommendation recommendation.SingleResourceRecommendation,
		containerName string,
		resourceName model.ResourceName,
		currentRequest model.ResourceAmount,
	) recommendation.SingleResourceRecommendation
}
```

A rule usually adjusts `Target`, `LowerBound`, and/or `UpperBound`. `UncappedTarget` should normally remain unchanged so callers can still inspect the estimator output before policy-level adjustment.

### Step 2. Add the rule name

In `logic/scalingrules/types.go`:

```go
const (
	BlockScaleUpRule   = "block-scale-up"
	BlockScaleDownRule = "block-scale-down"
	MyRuleName         = "my-rule"
)
```

### Step 3. Register it in `SelectScalingRule`

```go
func SelectScalingRule(name string) ScalingRule {
	switch name {
	case BlockScaleUpRule:
		return &BlockScaleUp{}
	case BlockScaleDownRule:
		return &BlockScaleDown{}
	case MyRuleName:
		return &MyRule{}
	default:
		return nil
	}
}
```

### Step 4. Update the CRD enum

In `charts/vhape-recommender/crds/vhapepolicy-crd.yaml`, add the new rule to the `scalingRule` enum:

```yaml
scalingRule:
  type: string
  enum:
    - ""
    - block-scale-up
    - block-scale-down
    - my-rule
```

### Step 5. Use it in a VHAPE policy

```yaml
spec:
  scalingRule: my-rule
```
