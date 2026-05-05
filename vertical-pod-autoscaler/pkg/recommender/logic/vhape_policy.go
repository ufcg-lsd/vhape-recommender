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

// DefaultVhapePolicy returns the default policy when none is specified.
func DefaultVhapePolicy() *VhapePolicy {
	return &VhapePolicy{
		Spec: VhapePolicySpec{
			Heuristic: HeuristicP93Hysteresis,
			CPU: VhapeResourceConfig{
				Percentile: 0.93,
				Headroom:   0.10,
			},
			Memory: VhapeResourceConfig{
				Percentile: 0.93,
				Headroom:   0.10,
			},
			ScalingRule: "",
		},
	}
}

// FetchVhapePolicy fetches a VhapePolicy from the cluster by name and namespace.
// If name is empty or the object is not found, returns the default policy.
func FetchVhapePolicy(client dynamic.Interface, namespace, name string) (*VhapePolicy, error) {
	if name == "" {
		klog.V(4).InfoS("VhapePolicy: nenhuma policy especificada, usando default")
		return DefaultVhapePolicy(), nil
	}

	unstructured, err := client.Resource(VhapePolicyGVR).Namespace(namespace).Get(
		context.TODO(), name, metav1.GetOptions{},
	)
	if err != nil {
		klog.V(4).InfoS("VhapePolicy: policy não encontrada, usando default", "name", name, "error", err)
		return DefaultVhapePolicy(), nil
	}

	spec, ok := unstructured.Object["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("VhapePolicy %s: campo spec inválido", name)
	}

	policy := DefaultVhapePolicy()
	policy.Name = name
	policy.Namespace = namespace

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
	}

	if memory, ok := spec["memory"].(map[string]interface{}); ok {
		if percentile, ok := memory["percentile"].(float64); ok {
			policy.Spec.Memory.Percentile = percentile
		}
		if headroom, ok := memory["headroom"].(float64); ok {
			policy.Spec.Memory.Headroom = headroom
		}
	}

	klog.V(4).InfoS("VhapePolicy carregada do cluster",
		"name", name,
		"heuristic", policy.Spec.Heuristic,
		"cpuPercentile", policy.Spec.CPU.Percentile,
		"cpuHeadroom", policy.Spec.CPU.Headroom,
		"memoryPercentile", policy.Spec.Memory.Percentile,
		"memoryHeadroom", policy.Spec.Memory.Headroom,
		"scalingRule", policy.Spec.ScalingRule,
	)

	return policy, nil
}