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

// VhapePolicyGVR identifies the VhapePolicy custom resource in the Kubernetes API. 
var VhapePolicyGVR = schema.GroupVersionResource{
	Group:    "autoscaling.vhape.io",
	Version:  "v1alpha1",
	Resource: "vhapepolicies",
}

// VhapePolicy is the parsed representation of a VhapePolicy custom resource.
type VhapePolicy struct {
	Name      string
	Namespace string
	Spec      VhapePolicySpec
}

// VhapePolicySpec describes the recommendation behavior configured by a VhapePolicy.
type VhapePolicySpec struct {
	Resources   VhapeResourcesSpec
	ScalingRule string
}

// VhapeResourcesSpec contains the heuristic configuration for each supported resource.
type VhapeResourcesSpec struct {
	CPU    VhapeResourceSpec
	Memory VhapeResourceSpec
}

// VhapeResourceSpec contains the heuristic used to recommend a single resource.
type VhapeResourceSpec struct {
	Heuristic ResourceHeuristicSpec
}

// ResourceHeuristicSpec represents a resource recommendation heuristic
// configured in a VhapePolicy.
//
// Each heuristic is responsible for creating the estimator used for one
// resource, such as CPU or memory.
type ResourceHeuristicSpec interface {
	NewEstimator(resourceName model.ResourceName) estimators.ResourceEstimator
}

// estimators names
const (
	PercentileHysteresis = "percentile-hysteresis"
)


// PercentileHysteresisSpec contains the configuration for the percentile hysteresis heuristic.
type PercentileHysteresisSpec struct {
	Percentile    float64
	Headroom      float64
	SlidingWindow time.Duration
}

// NewEstimator creates a percentile hysteresis estimator for the given resource.
func (s *PercentileHysteresisSpec) NewEstimator(resourceName model.ResourceName) estimators.ResourceEstimator {
	return estimators.NewPercentileHysteresisEstimator(
		resourceName,
		s.Percentile,
		s.Headroom,
		s.SlidingWindow,
	)
}


// FetchVhapePolicy loads and parses a VhapePolicy from the Kubernetes API.
//
// The policy is fetched from policyNamespace using policyName.
//
// The returned policy contains typed recommender configuration extracted from
// the unstructured Kubernetes object.
func FetchVhapePolicy(client dynamic.Interface, policyNamespace string, policyName string) (*VhapePolicy, error) {
	if policyNamespace == "" {
		return nil, fmt.Errorf("VhapePolicy: policy namespace not defined")
	}

	if policyName == "" {
		return nil, fmt.Errorf("VhapePolicy: policy name not defined")
	}

	unstructuredPolicy, err := client.Resource(VhapePolicyGVR).Namespace(policyNamespace).Get(
		context.TODO(), policyName, metav1.GetOptions{},
	)

	if err != nil {
		return nil, fmt.Errorf("VhapePolicy %q not found in namespace %q: %w", policyName, policyNamespace, err)
	}

	spec, ok := unstructuredPolicy.Object["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: invalid spec field", policyName)
	}

	resources, ok := spec["resources"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: missing spec.resources", policyName)
	}

	cpu, ok := resources["cpu"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: missing spec.resources.cpu", policyName)
	}

	memory, ok := resources["memory"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %q: missing spec.resources.memory", policyName)
	}

	cpuSpec, cpuHeuristicName, err := parseResourceSpec(cpu)
	if err != nil {
		return nil, fmt.Errorf("VhapePolicy %q: invalid spec.resources.cpu: %w", policyName, err)
	}

	memorySpec, memHeuristicName, err := parseResourceSpec(memory)
	if err != nil {
		return nil, fmt.Errorf("VhapePolicy %q: invalid spec.resources.memory: %w", policyName, err)
	}

	policy := &VhapePolicy{
		Name:      unstructuredPolicy.GetName(),
		Namespace: unstructuredPolicy.GetNamespace(),
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
