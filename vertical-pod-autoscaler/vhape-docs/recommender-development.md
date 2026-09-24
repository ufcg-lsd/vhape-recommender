# VHAPE Recommender development

This guide explains how to extend the VHAPE Recommender with new recommendation heuristics and scaling rules.

A heuristic is responsible for producing resource recommendations from usage data. A scaling rule is applied later, after the heuristic runs, to constrain one resource relative to the request captured when the VPA was first observed.

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
// This string is the YAML key used under scalingHeuristic for either resource.
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

Update `charts/vhape-recommender/crds/vhapepolicy-crd.yaml` so Kubernetes accepts the new heuristic under `scalingHeuristic` for both `cpu` and `memory`. This step is not optional: the schema is structural, so parameters the CRD does not declare are pruned by the API server before the recommender sees them, and the policy then fails with `must define exactly one heuristic`.

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
      scalingHeuristic:
        my-heuristic:
          someParameter: 1.0
    memory:
      scalingHeuristic:
        percentile-hysteresis:
          percentile: 0.93
          headroom: 0.10
          slidingWindow: 24h
          minimumCoverageWindow: 30m
```

## Adding a new scaling rule

A scaling rule transforms one `SingleResourceRecommendation` after the estimator runs and before CPU and memory are combined into a full container recommendation. It receives the original request captured in the VPA's `autoscaling.vhape.io/initial-requests` annotation, not the current workload request.

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
		originalRequest model.ResourceAmount,
	) recommendation.SingleResourceRecommendation
}
```

A rule usually adjusts `Target`, `LowerBound`, and/or `UpperBound`. `UncappedTarget` should normally remain unchanged so callers can still inspect the estimator output before policy-level adjustment.

### Step 2. Decode parameters and register the rule

Rules self-register from their implementation file. Define a stable name, a parameter struct, a factory, and an `init` function. `Factory` has the signature `func([]byte) (ScalingRule, error)`.

```go
const MyRule = "my-rule"

type myRule struct{ factor float64 }

func init() {
	Register(MyRule, newMyRule)
}

func newMyRule(raw []byte) (ScalingRule, error) {
	var parameters struct {
		Factor float64 `json:"factor"`
	}
	if err := json.Unmarshal(raw, &parameters); err != nil {
		return nil, fmt.Errorf("decode parameters: %w", err)
	}
	return myRule{factor: parameters.Factor}, nil
}

func (r myRule) Apply(
	rec recommendation.SingleResourceRecommendation,
	originalRequest model.ResourceAmount,
) recommendation.SingleResourceRecommendation {
	// Transform rec using originalRequest.
	return rec
}
```

`Register` rejects duplicate names at startup. `Build` decodes rules once when VHAPE creates its estimators, then applies them in the order written in the policy.

### Step 3. Update the CRD schema

In `charts/vhape-recommender/crds/vhapepolicy-crd.yaml`, add the rule and its parameter schema to `scalingRules.items.properties` for both `cpu` and `memory`. Each `scalingRules` item is an object with exactly one rule name.

```yaml
scalingRules:
  type: array
  items:
    type: object
    minProperties: 1
    maxProperties: 1
    properties:
      my-rule:
        type: object
        required:
          - factor
        properties:
          factor:
            type: number
            minimum: 0
```

### Step 4. Use it in a VHAPE policy

Configure rules below the resource they affect. Multiple entries are applied in list order.

```yaml
spec:
  resources:
    cpu:
      scalingRules:
        - my-rule:
            factor: 1.5
```
