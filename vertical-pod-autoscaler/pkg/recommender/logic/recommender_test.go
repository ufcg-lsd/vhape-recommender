package logic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	input_metrics "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/metrics"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/estimators"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/scalingrules"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

type mockMetricsClient struct {
	snapshots []*input_metrics.ContainerMetricsSnapshot
	err       error
}

func (m *mockMetricsClient) GetContainersMetrics(_ context.Context) ([]*input_metrics.ContainerMetricsSnapshot, error) {
	return m.snapshots, m.err
}

type mockResourceEstimator struct {
	recommendations map[string]recommendation.SingleResourceRecommendation
	feedCalls       map[string][][]model.ResourceAmount
}

func (m *mockResourceEstimator) FeedSamples(containerName string, samples []model.ResourceAmount) {
	if m.feedCalls == nil {
		m.feedCalls = make(map[string][][]model.ResourceAmount)
	}
	copied := append([]model.ResourceAmount(nil), samples...)
	m.feedCalls[containerName] = append(m.feedCalls[containerName], copied)
}

func (m *mockResourceEstimator) GetSingleResourceRecommendation(containerName string, _ estimators.ContainerResourceConstraints) recommendation.SingleResourceRecommendation {
	if r, ok := m.recommendations[containerName]; ok {
		return r
	}
	return recommendation.SingleResourceRecommendation{}
}

func newTestPolicyObject(namespace, name string, spec map[string]interface{}) *unstructured.Unstructured {
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

func newTestPolicySpec() map[string]interface{} {
	return map[string]interface{}{
		"resources": map[string]interface{}{
			"cpu": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.9,
					"headroom":      0.1,
					"slidingWindow": "1h",
				},
			},
			"memory": map[string]interface{}{
				PercentileHysteresis: map[string]interface{}{
					"percentile":    0.9,
					"headroom":      0.1,
					"slidingWindow": "1h",
				},
			},
		},
	}
}

func newTestVPA(namespace, name, policyRef string) *model.Vpa {
	vpa := model.NewVpa(
		model.VpaID{Namespace: namespace, VpaName: name},
		labels.Everything(),
		time.Now(),
	)
	if policyRef != "" {
		vpa.Annotations[vhapePolicyAnnotation] = policyRef
	}
	return vpa
}

func newEstimatorPolicy(namespace, name string) *VhapePolicy {
	return &VhapePolicy{
		Namespace: namespace,
		Name:      name,
		Spec: VhapePolicySpec{
			Resources: VhapeResourcesSpec{
				CPU: VhapeResourceSpec{Heuristic: &PercentileHysteresisSpec{
					Percentile:    0.9,
					Headroom:      0.1,
					SlidingWindow: time.Hour,
				}},
				Memory: VhapeResourceSpec{Heuristic: &PercentileHysteresisSpec{
					Percentile:    0.9,
					Headroom:      0.1,
					SlidingWindow: time.Hour,
				}},
			},
		},
	}
}

func TestFilterControlledResources(t *testing.T) {
	tests := []struct {
		name                string
		estimation          model.Resources
		controlledResources []model.ResourceName
		want                model.Resources
	}{
		{
			name: "keeps CPU and memory when both are controlled",
			estimation: model.Resources{
				model.ResourceCPU:    250,
				model.ResourceMemory: 512,
			},
			controlledResources: []model.ResourceName{model.ResourceCPU, model.ResourceMemory},
			want: model.Resources{
				model.ResourceCPU:    250,
				model.ResourceMemory: 512,
			},
		},
		{
			name: "keeps only memory when CPU is not controlled",
			estimation: model.Resources{
				model.ResourceCPU:    250,
				model.ResourceMemory: 512,
			},
			controlledResources: []model.ResourceName{model.ResourceMemory},
			want: model.Resources{
				model.ResourceMemory: 512,
			},
		},
		{
			name: "ignores unknown controlled resource",
			estimation: model.Resources{
				model.ResourceCPU: 250,
			},
			controlledResources: []model.ResourceName{model.ResourceName("nvidia.com/gpu")},
			want:                model.Resources{},
		},
		{
			name: "returns empty map when controlled resources is empty",
			estimation: model.Resources{
				model.ResourceCPU:    250,
				model.ResourceMemory: 512,
			},
			controlledResources: []model.ResourceName{},
			want:                model.Resources{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FilterControlledResources(tt.estimation, tt.controlledResources))
		})
	}
}

func TestMapToListOfRecommendedContainerResourcesSortsAndFormats(t *testing.T) {
	resources := RecommendedPodResources{
		"sidecar": recommendation.ResourceRecommendation{
			Target: model.Resources{
				model.ResourceCPU:    101,
				model.ResourceMemory: 2049,
			},
			LowerBound: model.Resources{
				model.ResourceCPU:    50,
				model.ResourceMemory: 1024,
			},
			UpperBound: model.Resources{
				model.ResourceCPU:    150,
				model.ResourceMemory: 4096,
			},
			UncappedTarget: model.Resources{
				model.ResourceCPU:    101,
				model.ResourceMemory: 2049,
			},
		},
		"app": recommendation.ResourceRecommendation{
			Target: model.Resources{
				model.ResourceCPU:    250,
				model.ResourceMemory: 8192,
			},
			LowerBound: model.Resources{
				model.ResourceCPU:    200,
				model.ResourceMemory: 4096,
			},
			UpperBound: model.Resources{
				model.ResourceCPU:    300,
				model.ResourceMemory: 12288,
			},
			UncappedTarget: model.Resources{
				model.ResourceCPU:    250,
				model.ResourceMemory: 8192,
			},
		},
	}

	got := MapToListOfRecommendedContainerResources(resources, RecommendationFormat{
		RoundCPUMillicores: 100,
		RoundMemoryBytes:   1024,
	})

	assert.Len(t, got.ContainerRecommendations, 2)
	assert.Equal(t, "app", got.ContainerRecommendations[0].ContainerName)
	assert.Equal(t, "sidecar", got.ContainerRecommendations[1].ContainerName)
	assert.True(t, quantityEqual(resource.MustParse("300m"), got.ContainerRecommendations[0].Target[v1.ResourceCPU]))
	assert.True(t, quantityEqual(resource.MustParse("8Ki"), got.ContainerRecommendations[0].Target[v1.ResourceMemory]))
	assert.True(t, quantityEqual(resource.MustParse("200m"), got.ContainerRecommendations[1].Target[v1.ResourceCPU]))
	assert.True(t, quantityEqual(resource.MustParse("3Ki"), got.ContainerRecommendations[1].Target[v1.ResourceMemory]))
}

func TestCalculateContainerConstraintsSplitPodMinimums(t *testing.T) {
	recommender := &podResourceRecommender{
		config: PodRecommendationLimits{
			PodMinCPUMillicores: 90,
			PodMinMemoryMb:      300,
		},
	}

	cpu := recommender.calculateContainerCpuConstraints(3, 250)
	assert.Equal(t, model.ResourceAmount(30), cpu.Min)
	assert.Equal(t, model.ResourceAmount(250), cpu.CurrentRequest)

	memory := recommender.calculateContainerMemoryConstraints(3, 512)
	assert.Equal(t, model.ResourceAmount(100*1024*1024), memory.Min)
	assert.Equal(t, model.ResourceAmount(512), memory.CurrentRequest)
}

func TestCalculateContainerCpuConstraintsZeroContainerCount(t *testing.T) {
	recommender := &podResourceRecommender{
		config: PodRecommendationLimits{
			PodMinCPUMillicores: 90,
			PodMinMemoryMb:      300,
		},
	}

	cpu := recommender.calculateContainerCpuConstraints(0, 250)
	assert.Equal(t, estimators.ContainerResourceConstraints{}, cpu)
}

func TestCalculateContainerMemoryConstraintsZeroContainerCount(t *testing.T) {
	recommender := &podResourceRecommender{
		config: PodRecommendationLimits{
			PodMinCPUMillicores: 90,
			PodMinMemoryMb:      300,
		},
	}

	memory := recommender.calculateContainerMemoryConstraints(0, 512)
	assert.Equal(t, estimators.ContainerResourceConstraints{}, memory)
}

func quantityEqual(want, got resource.Quantity) bool {
	return want.Cmp(got) == 0 && want.Format == got.Format
}

func TestApplyScalingRule(t *testing.T) {
	tests := []struct {
		name           string
		scalingRule    string
		currentRequest model.ResourceAmount
		input          recommendation.SingleResourceRecommendation
		want           recommendation.SingleResourceRecommendation
	}{
		{
			name:           "block scale up caps values above current request",
			scalingRule:    scalingrules.BlockScaleUpRule,
			currentRequest: 100,
			input: recommendation.SingleResourceRecommendation{
				Target:         120,
				LowerBound:     90,
				UpperBound:     130,
				UncappedTarget: 120,
			},
			want: recommendation.SingleResourceRecommendation{
				Target:         100,
				LowerBound:     90,
				UpperBound:     100,
				UncappedTarget: 120,
			},
		},
		{
			name:           "block scale down raises values below current request",
			scalingRule:    scalingrules.BlockScaleDownRule,
			currentRequest: 100,
			input: recommendation.SingleResourceRecommendation{
				Target:         80,
				LowerBound:     60,
				UpperBound:     110,
				UncappedTarget: 80,
			},
			want: recommendation.SingleResourceRecommendation{
				Target:         100,
				LowerBound:     100,
				UpperBound:     110,
				UncappedTarget: 80,
			},
		},
		{
			name:           "unknown rule leaves recommendation unchanged",
			scalingRule:    "not-a-rule",
			currentRequest: 100,
			input: recommendation.SingleResourceRecommendation{
				Target:         120,
				LowerBound:     90,
				UpperBound:     130,
				UncappedTarget: 120,
			},
			want: recommendation.SingleResourceRecommendation{
				Target:         120,
				LowerBound:     90,
				UpperBound:     130,
				UncappedTarget: 120,
			},
		},
	}

	recommender := &podResourceRecommender{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recommender.applyScalingRule(tt.input, &VhapePolicy{
				Spec: VhapePolicySpec{
					ScalingRule: tt.scalingRule,
				},
			}, "app", model.ResourceCPU, tt.currentRequest)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCollectCurrentUsageReturnsMetrics(t *testing.T) {
	podID := model.PodID{Namespace: "default", PodName: "pod-1"}
	now := time.Now()

	mc := &mockMetricsClient{
		snapshots: []*input_metrics.ContainerMetricsSnapshot{
			{
				ID:             model.ContainerID{PodID: podID, ContainerName: "app"},
				SnapshotTime:   now,
				SnapshotWindow: time.Minute,
				Usage: model.Resources{
					model.ResourceCPU:    200,
					model.ResourceMemory: 512,
				},
			},
		},
	}

	r := &podResourceRecommender{metricsClient: mc}
	usage, err := r.collectCurrentUsage([]model.PodID{podID})

	assert.NoError(t, err)
	assert.Equal(t, model.ResourceAmount(200), usage.CPU["app"][0])
	assert.Equal(t, model.ResourceAmount(512), usage.Memory["app"][0])
}

func TestCollectCurrentUsageFiltersNonMatchingPods(t *testing.T) {
	matchingPod := model.PodID{Namespace: "default", PodName: "pod-1"}
	otherPod := model.PodID{Namespace: "default", PodName: "pod-2"}
	now := time.Now()

	mc := &mockMetricsClient{
		snapshots: []*input_metrics.ContainerMetricsSnapshot{
			{
				ID:             model.ContainerID{PodID: matchingPod, ContainerName: "app"},
				SnapshotTime:   now,
				SnapshotWindow: time.Minute,
				Usage:          model.Resources{model.ResourceCPU: 100, model.ResourceMemory: 256},
			},
			{
				ID:             model.ContainerID{PodID: otherPod, ContainerName: "app"},
				SnapshotTime:   now,
				SnapshotWindow: time.Minute,
				Usage:          model.Resources{model.ResourceCPU: 300, model.ResourceMemory: 768},
			},
		},
	}

	r := &podResourceRecommender{metricsClient: mc}
	usage, err := r.collectCurrentUsage([]model.PodID{matchingPod})

	assert.NoError(t, err)
	assert.Len(t, usage.CPU["app"], 1)
	assert.Equal(t, model.ResourceAmount(100), usage.CPU["app"][0])
}

func TestCollectCurrentUsageReturnsErrorWithoutEmptyPoints(t *testing.T) {
	mc := &mockMetricsClient{err: assert.AnError}

	r := &podResourceRecommender{metricsClient: mc}
	usage, err := r.collectCurrentUsage([]model.PodID{{Namespace: "default", PodName: "pod-1"}})

	assert.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, usage.CPU)
	assert.Nil(t, usage.Memory)
}

func TestGetOrCreateEstimatorsCreatesNew(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "default/policy-a")
	policy := newEstimatorPolicy("default", "policy-a")
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	est := r.getOrCreateEstimators(vpa, policy)

	assert.NotNil(t, est.CPU)
	assert.NotNil(t, est.Memory)
	assert.Equal(t, "default", est.policyNamespace)
	assert.Equal(t, "policy-a", est.policyName)
}

func TestGetOrCreateEstimatorsReturnsCachedForSamePolicy(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "default/policy-a")
	policy := newEstimatorPolicy("default", "policy-a")
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	first := r.getOrCreateEstimators(vpa, policy)
	second := r.getOrCreateEstimators(vpa, policy)

	assert.Same(t, first, second)
}

func TestGetOrCreateEstimatorsRecreatesWhenPolicyChanges(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "default/policy-a")
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	first := r.getOrCreateEstimators(vpa, newEstimatorPolicy("default", "policy-a"))
	second := r.getOrCreateEstimators(vpa, newEstimatorPolicy("default", "policy-b"))

	assert.NotSame(t, first, second)
	assert.Equal(t, "policy-b", second.policyName)
	assert.Same(t, second, r.estimators[vpa.ID])
}

func TestGetOrCreateEstimatorsSeparatesByVPA(t *testing.T) {
	policy := newEstimatorPolicy("default", "policy-a")
	vpaA := newTestVPA("default", "vpa-a", "default/policy-a")
	vpaB := newTestVPA("default", "vpa-b", "default/policy-a")
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	first := r.getOrCreateEstimators(vpaA, policy)
	second := r.getOrCreateEstimators(vpaB, policy)

	assert.NotSame(t, first, second)
}

func TestFetchPolicyReturnsValidPolicy(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			VhapePolicyGVR: "VhapePolicyList",
		},
		newTestPolicyObject("default", "my-policy", newTestPolicySpec()),
	)

	r := &podResourceRecommender{dynamicClient: client}
	policy, err := r.fetchPolicy(newTestVPA("default", "my-vpa", "default/my-policy"))

	assert.NoError(t, err)
	assert.NotNil(t, policy)
	assert.Equal(t, "my-policy", policy.Name)
}

func TestFetchPolicyReturnsErrorForInvalidAnnotation(t *testing.T) {
	r := &podResourceRecommender{}

	policy, err := r.fetchPolicy(newTestVPA("default", "my-vpa", "invalid"))

	assert.Nil(t, policy)
	assert.ErrorContains(t, err, "invalid or missing vhape/policy annotation")
}

func TestFetchPolicyReturnsErrorWhenPolicyDoesNotExist(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			VhapePolicyGVR: "VhapePolicyList",
		},
	)

	r := &podResourceRecommender{dynamicClient: client}
	policy, err := r.fetchPolicy(newTestVPA("default", "my-vpa", "default/missing"))

	assert.Nil(t, policy)
	assert.Error(t, err)
	assert.ErrorContains(t, err, `fetch policy "default/missing"`)
}

func TestRecommendContainerResourcesComputesFullRecommendation(t *testing.T) {
	state := model.NewAggregateContainerState()
	state.ObserveRequest(model.Resources{
		model.ResourceCPU:    100,
		model.ResourceMemory: 256,
	}, time.Now())
	state.UpdateFromPolicy(&vpa_types.ContainerResourcePolicy{
		ControlledResources: &[]v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory},
	})

	cpuRec := recommendation.SingleResourceRecommendation{
		Target: 200, LowerBound: 180, UpperBound: 220, UncappedTarget: 200,
	}
	memRec := recommendation.SingleResourceRecommendation{
		Target: 512, LowerBound: 460, UpperBound: 564, UncappedTarget: 512,
	}

	est := &ResourceEstimators{
		CPU:    &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": cpuRec}},
		Memory: &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": memRec}},
	}

	policy := &VhapePolicy{Spec: VhapePolicySpec{Resources: VhapeResourcesSpec{
		CPU:    VhapeResourceSpec{Heuristic: &PercentileHysteresisSpec{Percentile: 0.9, Headroom: 0.1, SlidingWindow: time.Hour}},
		Memory: VhapeResourceSpec{Heuristic: &PercentileHysteresisSpec{Percentile: 0.9, Headroom: 0.1, SlidingWindow: time.Hour}},
	}}}

	r := &podResourceRecommender{config: PodRecommendationLimits{PodMinCPUMillicores: 50, PodMinMemoryMb: 100}}
	rec := r.recommendContainerResources("app", state, policy, est, 1)

	assert.Equal(t, model.ResourceAmount(200), rec.Target[model.ResourceCPU])
	assert.Equal(t, model.ResourceAmount(512), rec.Target[model.ResourceMemory])
}

func TestRecommendContainerResourcesFiltersControlledResources(t *testing.T) {
	state := model.NewAggregateContainerState()
	state.ObserveRequest(model.Resources{
		model.ResourceCPU:    100,
		model.ResourceMemory: 256,
	}, time.Now())
	state.UpdateFromPolicy(&vpa_types.ContainerResourcePolicy{
		ControlledResources: &[]v1.ResourceName{v1.ResourceMemory},
	})

	cpuRec := recommendation.SingleResourceRecommendation{
		Target: 200, LowerBound: 180, UpperBound: 220, UncappedTarget: 200,
	}
	memRec := recommendation.SingleResourceRecommendation{
		Target: 512, LowerBound: 460, UpperBound: 564, UncappedTarget: 512,
	}

	est := &ResourceEstimators{
		CPU:    &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": cpuRec}},
		Memory: &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": memRec}},
	}

	policy := &VhapePolicy{Spec: VhapePolicySpec{Resources: VhapeResourcesSpec{
		CPU:    VhapeResourceSpec{Heuristic: &PercentileHysteresisSpec{Percentile: 0.9, Headroom: 0.1, SlidingWindow: time.Hour}},
		Memory: VhapeResourceSpec{Heuristic: &PercentileHysteresisSpec{Percentile: 0.9, Headroom: 0.1, SlidingWindow: time.Hour}},
	}}}

	r := &podResourceRecommender{config: PodRecommendationLimits{PodMinCPUMillicores: 50, PodMinMemoryMb: 100}}
	rec := r.recommendContainerResources("app", state, policy, est, 1)

	assert.Empty(t, rec.Target[model.ResourceCPU])
	assert.Equal(t, model.ResourceAmount(512), rec.Target[model.ResourceMemory])
}

func TestGetRecommendedPodResourcesReturnsErrorForNilVPA(t *testing.T) {
	r := &podResourceRecommender{}

	got, err := r.GetRecommendedPodResources(nil, nil, nil)

	assert.Nil(t, got)
	assert.ErrorContains(t, err, "VPA is nil")
}

func TestGetRecommendedPodResourcesEmptyContainerStates(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{VhapePolicyGVR: "VhapePolicyList"},
		newTestPolicyObject("default", "policy", newTestPolicySpec()),
	)
	r := &podResourceRecommender{
		dynamicClient: client,
		estimators:    make(map[model.VpaID]*ResourceEstimators),
	}
	vpa := newTestVPA("default", "vpa", "default/policy")

	got, err := r.GetRecommendedPodResources(nil, vpa, nil)

	assert.NoError(t, err)
	assert.Empty(t, got)
	assert.Contains(t, r.estimators, vpa.ID)
}

func TestGetRecommendedPodResourcesReturnsPolicyError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{VhapePolicyGVR: "VhapePolicyList"},
	)
	r := &podResourceRecommender{
		dynamicClient: client,
		estimators:    make(map[model.VpaID]*ResourceEstimators),
	}
	containerStates := model.ContainerNameToAggregateStateMap{
		"app": model.NewAggregateContainerState(),
	}

	got, err := r.GetRecommendedPodResources(
		containerStates,
		newTestVPA("default", "vpa", "default/missing-policy"),
		nil,
	)

	assert.Nil(t, got)
	assert.Error(t, err)
}

func TestGetRecommendedPodResourcesDoesNotFeedEstimatorsWhenMetricsFail(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{VhapePolicyGVR: "VhapePolicyList"},
		newTestPolicyObject("default", "policy", newTestPolicySpec()),
	)
	vpa := newTestVPA("default", "vpa", "default/policy")
	cpuEstimator := &mockResourceEstimator{}
	memoryEstimator := &mockResourceEstimator{}
	r := &podResourceRecommender{
		dynamicClient: client,
		metricsClient: &mockMetricsClient{err: assert.AnError},
		estimators: map[model.VpaID]*ResourceEstimators{
			vpa.ID: {
				CPU:             cpuEstimator,
				Memory:          memoryEstimator,
				policyNamespace: "default",
				policyName:      "policy",
			},
		},
	}
	containerStates := model.ContainerNameToAggregateStateMap{
		"app": model.NewAggregateContainerState(),
	}

	got, err := r.GetRecommendedPodResources(
		containerStates,
		vpa,
		[]model.PodID{{Namespace: "default", PodName: "pod-1"}},
	)

	assert.Nil(t, got)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, cpuEstimator.feedCalls)
	assert.Empty(t, memoryEstimator.feedCalls)
}

func TestGetRecommendedPodResourcesRecreatesEstimatorsAfterPolicyAnnotationChange(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{VhapePolicyGVR: "VhapePolicyList"},
		newTestPolicyObject("default", "policy-a", newTestPolicySpec()),
		newTestPolicyObject("default", "policy-b", newTestPolicySpec()),
	)
	vpa := newTestVPA("default", "vpa", "default/policy-a")
	r := &podResourceRecommender{
		dynamicClient: client,
		estimators:    make(map[model.VpaID]*ResourceEstimators),
	}

	_, err := r.GetRecommendedPodResources(nil, vpa, nil)
	assert.NoError(t, err)
	first := r.estimators[vpa.ID]

	vpa.Annotations[vhapePolicyAnnotation] = "default/policy-b"
	_, err = r.GetRecommendedPodResources(nil, vpa, nil)
	assert.NoError(t, err)
	second := r.estimators[vpa.ID]

	assert.NotSame(t, first, second)
	assert.Equal(t, "policy-b", second.policyName)
}

func TestGetRecommendedPodResourcesFullPipeline(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			VhapePolicyGVR: "VhapePolicyList",
		},
		newTestPolicyObject("default", "my-policy", newTestPolicySpec()),
	)

	now := time.Now()
	podID := model.PodID{Namespace: "default", PodName: "pod-1"}
	
	mc := &mockMetricsClient{
		snapshots: []*input_metrics.ContainerMetricsSnapshot{
			{
				ID:             model.ContainerID{PodID: podID, ContainerName: "app"},
				SnapshotTime:   now,
				SnapshotWindow: time.Minute,
				Usage:          model.Resources{model.ResourceCPU: 200, model.ResourceMemory: 512},
			},
		},
	}
	r := &podResourceRecommender{
		config:        PodRecommendationLimits{PodMinCPUMillicores: 50, PodMinMemoryMb: 100},
		dynamicClient: client,
		metricsClient: mc,
		estimators:    make(map[model.VpaID]*ResourceEstimators),
	}
	state := model.NewAggregateContainerState()
	state.ObserveRequest(model.Resources{
		model.ResourceCPU:    200,
		model.ResourceMemory: 512,
	}, now)
	state.UpdateFromPolicy(&vpa_types.ContainerResourcePolicy{
		ControlledResources: &[]v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory},
	})
	containerStates := model.ContainerNameToAggregateStateMap{"app": state}

	got, err := r.GetRecommendedPodResources(
		containerStates,
		newTestVPA("default", "vpa", "default/my-policy"),
		[]model.PodID{podID},
	)

	assert.NoError(t, err)
	assert.Contains(t, got, "app")
	assert.NotNil(t, got["app"].Target)
	assert.NotNil(t, got["app"].UncappedTarget)
}

func TestCreatePodResourceRecommender(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			VhapePolicyGVR: "VhapePolicyList",
		},
	)
	mc := &mockMetricsClient{}

	rec := CreatePodResourceRecommender(PodRecommendationLimits{}, client, mc)
	assert.NotNil(t, rec)
}
