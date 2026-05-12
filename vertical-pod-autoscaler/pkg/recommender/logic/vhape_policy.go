package logic

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

// VhapePolicyGVR is the GroupVersionResource for the VhapePolicy CRD.
var VhapePolicyGVR = schema.GroupVersionResource{
	Group:    "autoscaling.vhape.io",
	Version:  "v1alpha1",
	Resource: "vhapepolicies",
}

// VhapeResourceConfig defines the configuration for a single resource.
type VhapeResourceConfig struct {
	Percentile float64
	Headroom   float64
	LowerBound float64
	UpperBound float64
}

// VhapePolicySpec defines the desired state of a VhapePolicy.
type VhapePolicySpec struct {
	Heuristic   string
	CPU         VhapeResourceConfig
	Memory      VhapeResourceConfig
	ScalingRule string
}

// VhapePolicy is the CRD object representation.
type VhapePolicy struct {
	Name      string
	Namespace string
	Spec      VhapePolicySpec
}

// FetchVhapePolicy fetches a VhapePolicy from the cluster by name and namespace.
// If name is empty or the object is not found, returns the default policy.
func FetchVhapePolicy(client dynamic.Interface, namespace, name string) (*VhapePolicy, error) {
	if name == "" {
        return nil, fmt.Errorf("VhapePolicy: annotation vhape/policy não definida no VPA")
    }

    unstructured, err := client.Resource(VhapePolicyGVR).Namespace("kube-system").Get(
        context.TODO(), name, metav1.GetOptions{},
    )

    if err != nil {
        return nil, fmt.Errorf("VhapePolicy %q não encontrada no namespace kube-system: %w", name, err)
    }

	spec, ok := unstructured.Object["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %s: campo spec inválido", name)
	}

	policy := &VhapePolicy{
		Name:      name,
		Namespace: namespace,
		Spec: VhapePolicySpec{
			Heuristic:   HeuristicPercentileHysteresis,
			ScalingRule: "",
			CPU:         VhapeResourceConfig{Percentile: 0.93, Headroom: 0.10, LowerBound: 0.10, UpperBound: 0.10},
			Memory:      VhapeResourceConfig{Percentile: 0.93, Headroom: 0.10, LowerBound: 0.10, UpperBound: 0.10},
		},
	}

	if heuristic, ok := spec["heuristic"].(string); ok {
		policy.Spec.Heuristic = heuristic
	}
	if scalingRule, ok := spec["scalingRule"].(string); ok {
		policy.Spec.ScalingRule = scalingRule
	}

	if cpu, ok := spec["cpu"].(map[string]interface{}); ok {
		if percentile, ok := cpu["percentile"].(float64); ok {
			policy.Spec.CPU.Percentile = percentile
		}
		if headroom, ok := cpu["headroom"].(float64); ok {
			policy.Spec.CPU.Headroom = headroom
		}
		if lowerBound, ok := cpu["lowerBound"].(float64); ok {
			policy.Spec.CPU.LowerBound = lowerBound
		}
		if upperBound, ok := cpu["upperBound"].(float64); ok {
			policy.Spec.CPU.UpperBound = upperBound
		}
	}

	if memory, ok := spec["memory"].(map[string]interface{}); ok {
		if percentile, ok := memory["percentile"].(float64); ok {
			policy.Spec.Memory.Percentile = percentile
		}
		if headroom, ok := memory["headroom"].(float64); ok {
			policy.Spec.Memory.Headroom = headroom
		}
		if lowerBound, ok := memory["lowerBound"].(float64); ok {
			policy.Spec.Memory.LowerBound = lowerBound
		}
		if upperBound, ok := memory["upperBound"].(float64); ok {
			policy.Spec.Memory.UpperBound = upperBound
		}
	}

	klog.V(4).InfoS("VhapePolicy carregada do cluster",
		"name", name,
		"heuristic", policy.Spec.Heuristic,
		"cpuPercentile", policy.Spec.CPU.Percentile,
		"cpuHeadroom", policy.Spec.CPU.Headroom,
		"cpuLowerBound", policy.Spec.CPU.LowerBound,
		"cpuUpperBound", policy.Spec.CPU.UpperBound,
		"memoryPercentile", policy.Spec.Memory.Percentile,
		"memoryHeadroom", policy.Spec.Memory.Headroom,
		"memoryLowerBound", policy.Spec.Memory.LowerBound,
		"memoryUpperBound", policy.Spec.Memory.UpperBound,
		"scalingRule", policy.Spec.ScalingRule,
	)

	return policy, nil
}