package logic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestFetchVhapePolicy(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), newVhapePolicyObject("default", "policy", map[string]interface{}{
		"resources": map[string]interface{}{
			"cpu": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.9,
					"headroom":      0.15,
					"slidingWindow": "5m",
				},
			},
			"memory": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.95,
					"headroom":      0.2,
					"slidingWindow": "10m",
				},
			},
		},
		"scalingRule": "block-scale-up",
	}))

	got, err := FetchVhapePolicy(client, "default", "policy")

	assert.NoError(t, err)
	assert.Equal(t, "policy", got.Name)
	assert.Equal(t, "default", got.Namespace)
	assert.Equal(t, "block-scale-up", got.Spec.ScalingRule)

	cpuSpec, ok := got.Spec.Resources.CPU.Heuristic.(*PercentileHysteresisSpec)
	assert.True(t, ok)
	assert.Equal(t, 0.9, cpuSpec.Percentile)
	assert.Equal(t, 0.15, cpuSpec.Headroom)
	assert.Equal(t, 5*time.Minute, cpuSpec.SlidingWindow)

	memorySpec, ok := got.Spec.Resources.Memory.Heuristic.(*PercentileHysteresisSpec)
	assert.True(t, ok)
	assert.Equal(t, 0.95, memorySpec.Percentile)
	assert.Equal(t, 0.2, memorySpec.Headroom)
	assert.Equal(t, 10*time.Minute, memorySpec.SlidingWindow)

	assert.NotNil(t, cpuSpec.NewEstimator(model.ResourceCPU))
}

func TestFetchVhapePolicyValidationErrors(t *testing.T) {
	validPolicy := newVhapePolicyObject("default", "policy", map[string]interface{}{
		"resources": map[string]interface{}{
			"cpu": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.9,
					"headroom":      0.15,
					"slidingWindow": "5m",
				},
			},
			"memory": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.95,
					"headroom":      0.2,
					"slidingWindow": "10m",
				},
			},
		},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), validPolicy)

	tests := []struct {
		name            string
		policyNamespace string
		policyName      string
		wantErr         string
	}{
		{
			name:            "empty namespace",
			policyNamespace: "",
			policyName:      "policy",
			wantErr:         "policy namespace not defined",
		},
		{
			name:            "empty name",
			policyNamespace: "default",
			policyName:      "",
			wantErr:         "policy name not defined",
		},
		{
			name:            "missing policy",
			policyNamespace: "default",
			policyName:      "missing",
			wantErr:         "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FetchVhapePolicy(client, tt.policyNamespace, tt.policyName)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestParseResourceSpecValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		raw     map[string]interface{}
		wantErr string
	}{
		{
			name:    "empty resource spec",
			raw:     map[string]interface{}{},
			wantErr: "must define exactly one heuristic",
		},
		{
			name: "multiple heuristics",
			raw: map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{},
				"other":              map[string]interface{}{},
			},
			wantErr: "must define exactly one heuristic",
		},
		{
			name: "heuristic config is not a map",
			raw: map[string]interface{}{
				PercentileHysteresis: "bad",
			},
			wantErr: "must be a map of parameters",
		},
		{
			name: "unknown heuristic",
			raw: map[string]interface{}{
				"unknown": map[string]interface{}{},
			},
			wantErr: "unsupported heuristic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseResourceSpec(tt.raw)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestParsePercentileHysteresisSpecValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		raw     map[string]interface{}
		wantErr string
	}{
		{
			name: "missing percentile",
			raw: map[string]interface{}{
				"headroom":      0.1,
				"slidingWindow": "5m",
			},
			wantErr: "missing percentile",
		},
		{
			name: "missing headroom",
			raw: map[string]interface{}{
				"percentile":    0.9,
				"slidingWindow": "5m",
			},
			wantErr: "missing headroom",
		},
		{
			name: "missing sliding window",
			raw: map[string]interface{}{
				"percentile": 0.9,
				"headroom":   0.1,
			},
			wantErr: "missing slidingWindow",
		},
		{
			name: "invalid sliding window",
			raw: map[string]interface{}{
				"percentile":    0.9,
				"headroom":      0.1,
				"slidingWindow": "soon",
			},
			wantErr: "invalid slidingWindow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePercentileHysteresisSpec(tt.raw)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestFetchVhapePolicyWithoutScalingRule(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), newVhapePolicyObject("default", "policy", map[string]interface{}{
		"resources": map[string]interface{}{
			"cpu": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.9,
					"headroom":      0.15,
					"slidingWindow": "5m",
				},
			},
			"memory": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.95,
					"headroom":      0.2,
					"slidingWindow": "10m",
				},
			},
		},
	}))

	got, err := FetchVhapePolicy(client, "default", "policy")

	assert.NoError(t, err)
	assert.Equal(t, "policy", got.Name)
	assert.Equal(t, "default", got.Namespace)
	assert.Equal(t, "", got.Spec.ScalingRule)

	cpuSpec, ok := got.Spec.Resources.CPU.Heuristic.(*PercentileHysteresisSpec)
	assert.True(t, ok)
	assert.Equal(t, 0.9, cpuSpec.Percentile)
	assert.Equal(t, 0.15, cpuSpec.Headroom)
	assert.Equal(t, 5*time.Minute, cpuSpec.SlidingWindow)
}

func newVhapePolicyObject(namespace, name string, spec map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "autoscaling.vhape.io/v1alpha1",
			"kind":       "VhapePolicy",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
			"spec": spec,
		},
	}
}
