# Development guide

This guide explains common development tasks in VHAPE: adding new recommendation heuristics adding new scaling rules, building the recommender image and pushing helm charts.

A heuristic is responsible for producing resource recommendations from usage data. A scaling rule is applied later, after the heuristic runs, to optionally constrain how the recommendation moves relative to the current request.

## Adding a new heuristic

A heuristic has two parts:

1. an estimator implementation in `logic/estimators`;
2. a policy parser in `logic/vhape_policy.go`.

The estimator performs the runtime recommendation logic. The policy parser converts the `VhapePolicy` YAML into a typed estimator configuration.

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

* how it stores samples;
* how it handles missing data;
* how it calculates `Target`, `LowerBound`, `UpperBound`, and `UncappedTarget`;
* how it applies `constraints.Min`, `constraints.Max`, and `constraints.CurrentRequest`, when relevant.

### Step 2. Create a heuristic config struct

In `logic/vhape_policy.go`, add a config struct:

```go
type MyHeuristicSpec struct {
	SomeParameter float64
}
```

Make it implement `ResourceHeuristicSpec`, defining a constructor:

```go
type ResourceHeuristicSpec interface {
	NewEstimator(resourceName model.ResourceName) estimators.ResourceEstimator
}
```

Add a constant for the heuristic name:

```go
const (
	PercentileHysteresis = "percentile-hysteresis"
	MyHeuristic          = "my-heuristic"
)
```

This string is the YAML key used in `VhapePolicy`.

### Step 3. Add a parser

Add a parser function in `logic/vhape_policy.go`:

```go
func parseMyHeuristicSpec(raw map[string]interface{}) (*MyHeuristicSpec, error) {
	// Parse and validate fields from the VhapePolicy YAML.
}
```

### Step 4. Register it in `parseResourceSpec`

Update the heuristic switch in `parseResourceSpec`:

```go
switch name {
case PercentileHysteresis:
	// PercentileHysteresis parsing.

case MyHeuristic:
	// MyHeuristic parsing.
}
```

### Step 5. Update the CRD schema

Update `yamls/vhapepolicy-crd.yaml` so Kubernetes accepts the new heuristic.

For example:

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

### Step 6. Create a policy using the new heuristic

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapePolicy
metadata:
  name: my-heuristic-policy
  namespace: kube-system
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

In `yamls/vhapepolicy-crd.yaml`, add the new rule to the `scalingRule` enum:

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
