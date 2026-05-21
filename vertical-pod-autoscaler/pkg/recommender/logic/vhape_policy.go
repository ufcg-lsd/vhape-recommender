package logic

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/estimators"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

// VhapePolicyGVR is the GroupVersionResource for the VhapePolicy CRD.
var VhapePolicyGVR = schema.GroupVersionResource{
	Group:    "autoscaling.vhape.io",
	Version:  "v1alpha1",
	Resource: "vhapepolicies",
}

// Vhape policy structs
type VhapePolicy struct {
	Name      string
	Namespace string
	Spec      VhapePolicySpec
}

type VhapePolicySpec struct {
	Resources   VhapeResourcesSpec
	ScalingRule string
}

type VhapeResourcesSpec struct {
	CPU    VhapeResourceSpec
	Memory VhapeResourceSpec
}

type VhapeResourceSpec struct {
	Heuristic ResourceHeuristicSpec
}

// Heuristics
type ResourceHeuristicSpec interface {
	NewEstimator(resourceName model.ResourceName) estimators.ResourceEstimator
}

// estimators names
const (
	PercentileHysteresis = "percentile-hysteresis"
)

// Percentile hysteresis heuristic
type PercentileHysteresisSpec struct {
	Percentile    float64
	Headroom      float64
	SlidingWindow time.Duration
}

func (s *PercentileHysteresisSpec) NewEstimator(resourceName model.ResourceName) estimators.ResourceEstimator {
	return estimators.NewPercentileHysteresisEstimator(
		resourceName,
		s.Percentile,
		s.Headroom,
		s.SlidingWindow,
	)
}

// Fetching and parsing
func FetchVhapePolicy(client dynamic.Interface, namespace, name string) (*VhapePolicy, error) {
	if name == "" {
		return nil, fmt.Errorf("VhapePolicy: annotation vhape/policy not defined")
	}

	unstructured, err := client.Resource(VhapePolicyGVR).Namespace("kube-system").Get(
		context.TODO(), name, metav1.GetOptions{},
	)

	if err != nil {
		return nil, fmt.Errorf("VhapePolicy %q not found in namespace kube-system: %w", name, err)
	}

	spec, ok := unstructured.Object["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: invalid spec field", name)
	}

	resources, ok := spec["resources"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: missing spec.resources", name)
	}

	cpu, ok := resources["cpu"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: missing spec.resources.cpu", name)
	}

	memory, ok := resources["memory"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: missing spec.resources.memory", name)
	}

	cpuSpec, cpuHeuristicName, err := parseResourceSpec(cpu)
	if err != nil {
		return nil, fmt.Errorf("VhapePolicy %q: invalid spec.resources.cpu: %w", name, err)
	}

	memorySpec, memHeuristicName, err := parseResourceSpec(memory)
	if err != nil {
		return nil, fmt.Errorf("VhapePolicy %q: invalid spec.resources.memory: %w", name, err)
	}

	policy := &VhapePolicy{
		Name:      name,
		Namespace: namespace,
		Spec: VhapePolicySpec{
			Resources: VhapeResourcesSpec{
				CPU:    cpuSpec,
				Memory: memorySpec,
			},
		},
	}

	if scalingRule, ok := spec["scalingRule"].(string); ok {
		policy.Spec.ScalingRule = scalingRule
	}

	klog.V(4).InfoS("VhapePolicy loaded from cluster",
		"name", policy.Name,
		"namespace", policy.Namespace,
		"cpuHeuristic", cpuHeuristicName,
		"memHeuristic", memHeuristicName,
		"scalingRule", policy.Spec.ScalingRule,
	)

	return policy, nil
}

func parseResourceSpec(raw map[string]interface{}) (VhapeResourceSpec, string, error) {
	if len(raw) != 1 {
		return VhapeResourceSpec{}, "", fmt.Errorf("resource spec must define exactly one heuristic")
	}

	var name string
	var value interface{}
	for k, v := range raw {
		name = k
		value = v
	}

	config, ok := value.(map[string]interface{})
	if !ok {
		return VhapeResourceSpec{}, "", fmt.Errorf("heuristic %q must be a map of parameters", name)
	}

	switch name {
		case PercentileHysteresis:
			spec, err := parsePercentileHysteresisSpec(config)
			if err != nil {
				return VhapeResourceSpec{}, "", err
			}

			return VhapeResourceSpec{Heuristic: spec}, PercentileHysteresis, nil

		default:
			return VhapeResourceSpec{}, "", fmt.Errorf("unsupported heuristic %q", name)
	}
}

func parsePercentileHysteresisSpec(raw map[string]interface{}) (*PercentileHysteresisSpec, error) {
	percentile, ok := raw["percentile"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing percentile")
	}

	headroom, ok := raw["headroom"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing headroom")
	}

	slidingWindowRaw, ok := raw["slidingWindow"].(string)
	if !ok {
		return nil, fmt.Errorf("missing slidingWindow")
	}

	slidingWindow, err := time.ParseDuration(slidingWindowRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid slidingWindow %q: %w", slidingWindowRaw, err)
	}

	return &PercentileHysteresisSpec{
		Percentile:    percentile,
		Headroom:      headroom,
		SlidingWindow: slidingWindow,
	}, nil
}
