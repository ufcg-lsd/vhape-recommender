package logic

import (
	"context"
	"testing"
	"time"

	"fmt"

	"github.com/stretchr/testify/assert"
	autoscaling "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhape_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vhape_listers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.vhape.io/v1alpha1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/history"
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
	historyCalls    map[string][]estimators.TimedSample
	recommendCalls  map[string]int
}

func (m *mockResourceEstimator) FeedSamples(containerName string, samples []model.ResourceAmount) {
	if m.feedCalls == nil {
		m.feedCalls = make(map[string][][]model.ResourceAmount)
	}
	copied := append([]model.ResourceAmount(nil), samples...)
	m.feedCalls[containerName] = append(m.feedCalls[containerName], copied)
}

func (m *mockResourceEstimator) LoadHistory(containerName string, samples []estimators.TimedSample) {
	if m.historyCalls == nil {
		m.historyCalls = make(map[string][]estimators.TimedSample)
	}
	m.historyCalls[containerName] = append(m.historyCalls[containerName], samples...)
}

func (m *mockResourceEstimator) GetSingleResourceRecommendation(containerName string, _ estimators.ContainerResourceConstraints) recommendation.SingleResourceRecommendation {
	if m.recommendCalls == nil {
		m.recommendCalls = make(map[string]int)
	}
	m.recommendCalls[containerName]++
	if r, ok := m.recommendations[containerName]; ok {
		return r
	}
	return recommendation.SingleResourceRecommendation{}
}

func newTestPolicyObject(name string, spec vhape_types.VhapePolicySpec) *vhape_types.VhapePolicy {
	return &vhape_types.VhapePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID("uid-" + name),
		},
		Spec: spec,
	}
}

func newTestPolicySpec() vhape_types.VhapePolicySpec {
	return vhape_types.VhapePolicySpec{
		Resources: vhape_types.VhapeResourcesSpec{
			CPU:    newPercentileHysteresisHeuristic(0.9, 0.1, "1h"),
			Memory: newPercentileHysteresisHeuristic(0.9, 0.1, "1h"),
		},
	}
}

func newTestVPA(namespace, name, policyName string) *model.Vpa {
	vpa := model.NewVpa(
		model.VpaID{Namespace: namespace, VpaName: name},
		labels.Everything(),
		time.Now(),
	)
	if policyName != "" {
		vpa.Annotations[vhapePolicyAnnotation] = policyName
	}
	return vpa
}

// newVhapePolicyLister builds a lister backed by an indexer holding the given
// policies, which is what the informer cache provides at runtime.
func newVhapePolicyLister(policies ...*vhape_types.VhapePolicy) vhape_listers.VhapePolicyLister {
	indexer := cache.NewIndexer(
		cache.MetaNamespaceKeyFunc,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	for _, policy := range policies {
		if err := indexer.Add(policy); err != nil {
			panic(err)
		}
	}

	return vhape_listers.NewVhapePolicyLister(indexer)
}

func newPercentileHysteresisHeuristic(percentile, headroom float64, slidingWindow string) vhape_types.ResourceScalingSpec {
	return vhape_types.ResourceScalingSpec{
		ScalingHeuristic: vhape_types.ResourceHeuristics{
			estimators.PercentileHysteresis: runtime.RawExtension{
				Raw: []byte(fmt.Sprintf(
					`{"percentile":%v,"headroom":%v,"slidingWindow":%q}`,
					percentile, headroom, slidingWindow,
				)),
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

func TestGetOrCreateEstimatorsCreatesNew(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "policy-a")
	policy := newTestPolicyObject("policy-a", newTestPolicySpec())
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	est, created, err := r.getOrCreateEstimators(vpa, policy)

	assert.NoError(t, err)
	assert.True(t, created)
	assert.NotNil(t, est.CPU)
	assert.NotNil(t, est.Memory)

	assert.Equal(
		t,
		types.UID("uid-policy-a"),
		est.policyUID,
	)
}

func TestGetOrCreateEstimatorsReturnsCachedForSamePolicy(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "policy-a")
	policy := newTestPolicyObject("policy-a", newTestPolicySpec())
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	first, _, firstErr := r.getOrCreateEstimators(vpa, policy)
	second, created, secondErr := r.getOrCreateEstimators(vpa, policy)

	assert.NoError(t, firstErr)
	assert.NoError(t, secondErr)
	assert.Same(t, first, second)
	assert.False(t, created)
}

func TestGetOrCreateEstimatorsRecreatesWhenPolicyChanges(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "policy-a")
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	first, _, firstErr := r.getOrCreateEstimators(vpa, newTestPolicyObject("policy-a", newTestPolicySpec()))
	second, _, secondErr := r.getOrCreateEstimators(vpa, newTestPolicyObject("policy-b", newTestPolicySpec()))

	assert.NoError(t, firstErr)
	assert.NoError(t, secondErr)
	assert.NotSame(t, first, second)
	assert.Equal(
		t,
		types.UID("uid-policy-b"),
		second.policyUID,
	)
	assert.Same(t, second, r.estimators[vpa.ID])
}

func TestGetOrCreateEstimatorsSeparatesByVPA(t *testing.T) {
	policy := newTestPolicyObject("policy-a", newTestPolicySpec())
	vpaA := newTestVPA("default", "vpa-a", "policy-a")
	vpaB := newTestVPA("default", "vpa-b", "policy-a")
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	first, _, firstErr := r.getOrCreateEstimators(vpaA, policy)
	second, _, secondErr := r.getOrCreateEstimators(vpaB, policy)

	assert.NoError(t, firstErr)
	assert.NoError(t, secondErr)
	assert.NotSame(t, first, second)
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

	cpuEstimator := &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": cpuRec}}
	memoryEstimator := &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": memRec}}
	est := &ResourceEstimators{
		CPU: resourceEstimator{
			estimator: cpuEstimator,
		},
		Memory: resourceEstimator{
			estimator: memoryEstimator,
		},
	}

	r := &podResourceRecommender{config: PodRecommendationLimits{PodMinCPUMillicores: 50, PodMinMemoryMb: 100}}
	rec, err := r.recommendContainerResources("app", state, newTestVPA("default", "vpa", ""), est, 1)
	assert.NoError(t, err)

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

	cpuEstimator := &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": cpuRec}}
	memoryEstimator := &mockResourceEstimator{recommendations: map[string]recommendation.SingleResourceRecommendation{"app": memRec}}
	est := &ResourceEstimators{
		CPU: resourceEstimator{
			estimator: cpuEstimator,
		},
		Memory: resourceEstimator{
			estimator: memoryEstimator,
		},
	}

	r := &podResourceRecommender{config: PodRecommendationLimits{PodMinCPUMillicores: 50, PodMinMemoryMb: 100}}
	rec, err := r.recommendContainerResources("app", state, newTestVPA("default", "vpa", ""), est, 1)
	assert.NoError(t, err)

	assert.Empty(t, rec.Target[model.ResourceCPU])
	assert.Equal(t, model.ResourceAmount(512), rec.Target[model.ResourceMemory])
	assert.Empty(t, cpuEstimator.recommendCalls)
	assert.Equal(t, 1, memoryEstimator.recommendCalls["app"])
}

func TestGetRecommendedPodResourcesReturnsErrorForNilVPA(t *testing.T) {
	r := &podResourceRecommender{}

	got, err := r.GetRecommendedPodResources(nil, nil, nil)

	assert.Nil(t, got)
	assert.ErrorContains(t, err, "VPA is nil")
}

func TestGetRecommendedPodResourcesEmptyContainerStates(t *testing.T) {
	lister := newVhapePolicyLister(newTestPolicyObject("policy", newTestPolicySpec()))
	r := &podResourceRecommender{
		policyLister: lister,
		estimators:   make(map[model.VpaID]*ResourceEstimators),
	}
	vpa := newTestVPA("default", "vpa", "policy")

	got, err := r.GetRecommendedPodResources(nil, vpa, nil)

	assert.NoError(t, err)
	assert.Empty(t, got)
	assert.Contains(t, r.estimators, vpa.ID)
}

func TestGetRecommendedPodResourcesSkipsScalingRulesUntilInitialRequestsAreCached(t *testing.T) {
	policy := newTestPolicyObject("policy", newTestPolicySpec())
	policy.Spec.Resources.CPU.ScalingRules = []vhape_types.ScalingRule{{
		scalingrules.RequestCeilingRule: {Raw: []byte(`{"value":"100%"}`)},
	}}
	vpa := newTestVPA("default", "vpa", policy.Name)
	state := model.NewAggregateContainerState()
	state.ObserveRequest(model.Resources{model.ResourceCPU: 100}, time.Now())
	r := &podResourceRecommender{
		policyLister:  newVhapePolicyLister(policy),
		metricsClient: &mockMetricsClient{},
		estimators:    make(map[model.VpaID]*ResourceEstimators),
	}

	got, err := r.GetRecommendedPodResources(
		model.ContainerNameToAggregateStateMap{"app": state},
		vpa,
		nil,
	)

	assert.NoError(t, err)
	assert.Contains(t, got, "app")
}

func TestGetRecommendedPodResourcesReturnsPolicyError(t *testing.T) {
	lister := newVhapePolicyLister()
	r := &podResourceRecommender{
		policyLister: lister,
		estimators:   make(map[model.VpaID]*ResourceEstimators),
	}
	containerStates := model.ContainerNameToAggregateStateMap{
		"app": model.NewAggregateContainerState(),
	}

	got, err := r.GetRecommendedPodResources(
		containerStates,
		newTestVPA("default", "vpa", "missing-policy"),
		nil,
	)

	assert.Nil(t, got)
	assert.Error(t, err)
}

func TestGetRecommendedPodResourcesDoesNotFeedEstimatorsWhenMetricsFail(t *testing.T) {
	lister := newVhapePolicyLister(newTestPolicyObject("policy", newTestPolicySpec()))
	vpa := newTestVPA("default", "vpa", "policy")
	cpuEstimator := &mockResourceEstimator{}
	memoryEstimator := &mockResourceEstimator{}
	r := &podResourceRecommender{
		policyLister:  lister,
		metricsClient: &mockMetricsClient{err: assert.AnError},
		estimators: map[model.VpaID]*ResourceEstimators{
			vpa.ID: {
				CPU:       resourceEstimator{estimator: cpuEstimator},
				Memory:    resourceEstimator{estimator: memoryEstimator},
				policyUID: types.UID("uid-policy"),
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
	lister := newVhapePolicyLister(newTestPolicyObject("policy-a", newTestPolicySpec()),
		newTestPolicyObject("policy-b", newTestPolicySpec()),
	)
	vpa := newTestVPA("default", "vpa", "policy-a")
	r := &podResourceRecommender{
		policyLister: lister,
		estimators:   make(map[model.VpaID]*ResourceEstimators),
	}

	_, err := r.GetRecommendedPodResources(nil, vpa, nil)
	assert.NoError(t, err)
	first := r.estimators[vpa.ID]

	vpa.Annotations[vhapePolicyAnnotation] = "policy-b"
	_, err = r.GetRecommendedPodResources(nil, vpa, nil)
	assert.NoError(t, err)
	second := r.estimators[vpa.ID]

	assert.NotSame(t, first, second)
	assert.Equal(
		t,
		types.UID("uid-policy-b"),
		second.policyUID,
	)
}

func TestGetOrCreateEstimatorsRecreatesWhenPolicyUIDChanges(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "policy-a")
	r := &podResourceRecommender{
		estimators: make(map[model.VpaID]*ResourceEstimators),
	}

	firstPolicy := newTestPolicyObject("policy-a", newTestPolicySpec())
	firstPolicy.UID = types.UID("policy-uid-1")

	secondPolicy := newTestPolicyObject("policy-a", newTestPolicySpec())
	secondPolicy.UID = types.UID("policy-uid-2")

	first, _, firstErr := r.getOrCreateEstimators(vpa, firstPolicy)
	second, _, secondErr := r.getOrCreateEstimators(vpa, secondPolicy)

	assert.NoError(t, firstErr)
	assert.NoError(t, secondErr)
	assert.NotSame(t, first, second)
	assert.Equal(t, types.UID("policy-uid-2"), second.policyUID)
	assert.Same(t, second, r.estimators[vpa.ID])
}

func TestGetRecommendedPodResourcesFullPipeline(t *testing.T) {
	lister := newVhapePolicyLister(newTestPolicyObject("my-policy", newTestPolicySpec()))

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
		policyLister:  lister,
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
		newTestVPA("default", "vpa", "my-policy"),
		[]model.PodID{podID},
	)

	assert.NoError(t, err)
	assert.Contains(t, got, "app")
	assert.NotNil(t, got["app"].Target)
	assert.NotNil(t, got["app"].UncappedTarget)
}

func TestCreatePodResourceRecommender(t *testing.T) {
	lister := newVhapePolicyLister()
	mc := &mockMetricsClient{}

	rec := CreatePodResourceRecommender(PodRecommendationLimits{}, lister, mc, nil)
	assert.NotNil(t, rec)
}

func TestFreeRemovesOnlyRequestedVPAEstimators(t *testing.T) {
	vpaA := newTestVPA("default", "vpa-a", "policy")
	vpaB := newTestVPA("default", "vpa-b", "policy")

	r := &podResourceRecommender{
		estimators: map[model.VpaID]*ResourceEstimators{
			vpaA.ID: &ResourceEstimators{},
			vpaB.ID: &ResourceEstimators{},
		},
	}

	r.Free(vpaA.ID)

	assert.NotContains(t, r.estimators, vpaA.ID)
	assert.Contains(t, r.estimators, vpaB.ID)
}

func TestFreeDoesNothingWhenEstimatorsDoNotExist(t *testing.T) {
	vpaID := model.VpaID{
		Namespace: "default",
		VpaName:   "missing-vpa",
	}

	r := &podResourceRecommender{
		estimators: make(map[model.VpaID]*ResourceEstimators),
	}

	assert.NotPanics(t, func() {
		r.Free(vpaID)
		r.Free(vpaID)
	})

	assert.Empty(t, r.estimators)
}

func TestGetOrCreateEstimatorsBuildsEstimatorsFromPolicyHeuristics(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "policy-a")
	r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

	est, _, err := r.getOrCreateEstimators(vpa, newTestPolicyObject("policy-a", newTestPolicySpec()))

	assert.NoError(t, err)
	assert.IsType(t, &estimators.PercentileHysteresisEstimator{}, est.CPU.estimator)
	assert.IsType(t, &estimators.PercentileHysteresisEstimator{}, est.Memory.estimator)
}

func TestGetOrCreateEstimatorsReturnsErrorForInvalidHeuristics(t *testing.T) {
	tests := []struct {
		name    string
		spec    vhape_types.VhapePolicySpec
		wantErr string
	}{
		{
			name: "cpu without heuristic",
			spec: vhape_types.VhapePolicySpec{
				Resources: vhape_types.VhapeResourcesSpec{
					Memory: newPercentileHysteresisHeuristic(0.9, 0.1, "1h"),
				},
			},
			wantErr: `VhapePolicy "policy": invalid spec.resources.cpu: resource spec must define exactly one heuristic`,
		},
		{
			name: "unsupported memory heuristic",
			spec: vhape_types.VhapePolicySpec{
				Resources: vhape_types.VhapeResourcesSpec{
					CPU: newPercentileHysteresisHeuristic(0.9, 0.1, "1h"),
					Memory: vhape_types.ResourceScalingSpec{ScalingHeuristic: vhape_types.ResourceHeuristics{
						"unknown": {Raw: []byte(`{}`)},
					}},
				},
			},
			wantErr: `VhapePolicy "policy": invalid spec.resources.memory: unsupported heuristic "unknown"`,
		},
		{
			name: "malformed cpu parameters",
			spec: vhape_types.VhapePolicySpec{
				Resources: vhape_types.VhapeResourcesSpec{
					CPU: vhape_types.ResourceScalingSpec{ScalingHeuristic: vhape_types.ResourceHeuristics{
						estimators.PercentileHysteresis: runtime.RawExtension{
							Raw: []byte(`{"slidingWindow":"soon"}`),
						},
					}},
					Memory: newPercentileHysteresisHeuristic(0.9, 0.1, "1h"),
				},
			},
			wantErr: `VhapePolicy "policy": invalid spec.resources.cpu: heuristic "percentile-hysteresis": invalid parameters`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vpa := newTestVPA("default", "my-vpa", "policy")
			r := &podResourceRecommender{estimators: make(map[model.VpaID]*ResourceEstimators)}

			est, _, err := r.getOrCreateEstimators(vpa, newTestPolicyObject("policy", tt.spec))

			assert.Nil(t, est)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.NotContains(t, r.estimators, vpa.ID)
		})
	}
}

// A cache hit must return before the heuristics are resolved, so a policy that
// could not be resolved at all still serves the cached estimators.
func TestGetOrCreateEstimatorsSkipsHeuristicResolutionOnCacheHit(t *testing.T) {
	vpa := newTestVPA("default", "my-vpa", "policy")
	policy := newTestPolicyObject("policy", vhape_types.VhapePolicySpec{})
	cached := &ResourceEstimators{policyUID: policy.UID}
	r := &podResourceRecommender{
		estimators: map[model.VpaID]*ResourceEstimators{vpa.ID: cached},
	}

	est, _, err := r.getOrCreateEstimators(vpa, policy)

	assert.NoError(t, err)
	assert.Same(t, cached, est)
}

func usageSamples(start time.Time, cpu, memory model.ResourceAmount) []model.ContainerUsageSample {
	return []model.ContainerUsageSample{
		{MeasureStart: start, Usage: cpu, Resource: model.ResourceCPU},
		{MeasureStart: start, Usage: memory, Resource: model.ResourceMemory},
	}
}

func newMockEstimators() (*ResourceEstimators, *mockResourceEstimator, *mockResourceEstimator) {
	cpuEstimator := &mockResourceEstimator{}
	memoryEstimator := &mockResourceEstimator{}
	return &ResourceEstimators{
		CPU:       resourceEstimator{estimator: cpuEstimator},
		Memory:    resourceEstimator{estimator: memoryEstimator},
		policyUID: types.UID("uid-policy"),
	}, cpuEstimator, memoryEstimator
}

func TestLoadStartupHistoryLoadsMatchingPods(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	vpa := model.NewVpa(
		model.VpaID{Namespace: "default", VpaName: "vpa"},
		labels.SelectorFromSet(labels.Set{"app": "web"}),
		time.Now(),
	)
	livePod := model.PodID{Namespace: "default", PodName: "web-live"}
	r := &podResourceRecommender{startupHistory: map[model.PodID]*history.PodHistory{
		// Live pod, unknown labels: matched through the VPA's live pods.
		livePod: {Samples: map[string][]model.ContainerUsageSample{
			"app": usageSamples(start, 100, 1000),
		}},
		// Gone pod: matched through its last known labels.
		{Namespace: "default", PodName: "web-gone"}: {
			LastLabels: map[string]string{"app": "web"},
			Samples: map[string][]model.ContainerUsageSample{
				"app":     usageSamples(start, 200, 2000),
				"sidecar": usageSamples(start, 999, 9999),
			},
		},
		{Namespace: "default", PodName: "db"}: {
			LastLabels: map[string]string{"app": "db"},
			Samples:    map[string][]model.ContainerUsageSample{"app": usageSamples(start, 999, 9999)},
		},
		{Namespace: "other", PodName: "web"}: {
			LastLabels: map[string]string{"app": "web"},
			Samples:    map[string][]model.ContainerUsageSample{"app": usageSamples(start, 999, 9999)},
		},
	}}
	est, cpuEstimator, memoryEstimator := newMockEstimators()

	r.loadStartupHistory(
		vpa,
		[]model.PodID{livePod},
		model.ContainerNameToAggregateStateMap{"app": model.NewAggregateContainerState()},
		est,
	)

	assert.ElementsMatch(t, []estimators.TimedSample{
		{Value: 100, Timestamp: start},
		{Value: 200, Timestamp: start},
	}, cpuEstimator.historyCalls["app"])
	assert.ElementsMatch(t, []estimators.TimedSample{
		{Value: 1000, Timestamp: start},
		{Value: 2000, Timestamp: start},
	}, memoryEstimator.historyCalls["app"])
	assert.NotContains(t, cpuEstimator.historyCalls, "sidecar")
}

func TestLoadStartupHistoryMatchesGonePodsByOwner(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	vpa := model.NewVpa(
		model.VpaID{Namespace: "default", VpaName: "vpa"},
		labels.SelectorFromSet(labels.Set{"app": "web"}),
		time.Now(),
	)
	vpa.TargetRef = &autoscaling.CrossVersionObjectReference{Kind: "Deployment", Name: "web", APIVersion: "apps/v1"}
	ownedByWeb := []history.PodOwner{{Kind: "ReplicaSet", Name: "web-7d4b9c"}, {Kind: "Deployment", Name: "web"}}
	r := &podResourceRecommender{startupHistory: map[model.PodID]*history.PodHistory{
		// Gone pod without labels: matched through its owners.
		{Namespace: "default", PodName: "web-7d4b9c-abcde"}: {
			Owners:  ownedByWeb,
			Samples: map[string][]model.ContainerUsageSample{"app": usageSamples(start, 100, 1000)},
		},
		{Namespace: "other", PodName: "web-7d4b9c-abcde"}: {
			Owners:  ownedByWeb,
			Samples: map[string][]model.ContainerUsageSample{"app": usageSamples(start, 999, 9999)},
		},
		{Namespace: "default", PodName: "api-5f6c7d-fghij"}: {
			Owners:  []history.PodOwner{{Kind: "ReplicaSet", Name: "api-5f6c7d"}, {Kind: "Deployment", Name: "api"}},
			Samples: map[string][]model.ContainerUsageSample{"app": usageSamples(start, 999, 9999)},
		},
		{Namespace: "default", PodName: "web"}: {
			Owners:  []history.PodOwner{{Kind: "StatefulSet", Name: "web"}},
			Samples: map[string][]model.ContainerUsageSample{"app": usageSamples(start, 999, 9999)},
		},
	}}
	est, cpuEstimator, memoryEstimator := newMockEstimators()

	r.loadStartupHistory(
		vpa,
		nil,
		model.ContainerNameToAggregateStateMap{"app": model.NewAggregateContainerState()},
		est,
	)

	assert.Equal(t, []estimators.TimedSample{{Value: 100, Timestamp: start}}, cpuEstimator.historyCalls["app"])
	assert.Equal(t, []estimators.TimedSample{{Value: 1000, Timestamp: start}}, memoryEstimator.historyCalls["app"])
}

func TestLoadStartupHistoryLoadsNothingAfterRelease(t *testing.T) {
	vpa := newTestVPA("default", "vpa", "policy")
	podID := model.PodID{Namespace: "default", PodName: "pod"}
	r := &podResourceRecommender{startupHistory: map[model.PodID]*history.PodHistory{
		podID: {Samples: map[string][]model.ContainerUsageSample{
			"app": usageSamples(time.Now().Add(-time.Hour), 100, 1000),
		}},
	}}
	est, cpuEstimator, _ := newMockEstimators()

	r.ReleaseHistory()
	r.loadStartupHistory(
		vpa,
		[]model.PodID{podID},
		model.ContainerNameToAggregateStateMap{"app": model.NewAggregateContainerState()},
		est,
	)

	assert.Nil(t, r.startupHistory)
	assert.Empty(t, cpuEstimator.historyCalls)
}

// Only estimators created in the call are seeded, so the history is never
// loaded twice into the same estimators.
func TestGetRecommendedPodResourcesDoesNotSeedCachedEstimators(t *testing.T) {
	vpa := newTestVPA("default", "vpa", "policy")
	podID := model.PodID{Namespace: "default", PodName: "pod"}
	est, cpuEstimator, _ := newMockEstimators()
	r := &podResourceRecommender{
		policyLister:  newVhapePolicyLister(newTestPolicyObject("policy", newTestPolicySpec())),
		metricsClient: &mockMetricsClient{},
		estimators:    map[model.VpaID]*ResourceEstimators{vpa.ID: est},
		startupHistory: map[model.PodID]*history.PodHistory{
			podID: {Samples: map[string][]model.ContainerUsageSample{
				"app": usageSamples(time.Now().Add(-time.Hour), 100, 1000),
			}},
		},
	}

	_, err := r.GetRecommendedPodResources(
		model.ContainerNameToAggregateStateMap{"app": model.NewAggregateContainerState()},
		vpa,
		[]model.PodID{podID},
	)

	assert.NoError(t, err)
	assert.Empty(t, cpuEstimator.historyCalls)
}

// With the real estimator, the history read at startup gives enough coverage
// for a recommendation in the first cycle, instead of the current request.
func TestGetRecommendedPodResourcesRecommendsFromHistoryRightAfterStart(t *testing.T) {
	policy := newTestPolicyObject("policy", vhape_types.VhapePolicySpec{
		Resources: vhape_types.VhapeResourcesSpec{
			CPU: vhape_types.ResourceScalingSpec{ScalingHeuristic: vhape_types.ResourceHeuristics{
				estimators.PercentileHysteresis: runtime.RawExtension{
					Raw: []byte(`{"percentile":1,"headroom":0,"slidingWindow":"24h","minimumCoverageWindow":"30m"}`),
				},
			}},
			Memory: newPercentileHysteresisHeuristic(0.9, 0.1, "24h"),
		},
	})
	vpa := newTestVPA("default", "vpa", policy.Name)
	podID := model.PodID{Namespace: "default", PodName: "pod"}
	now := time.Now()
	r := &podResourceRecommender{
		config:        PodRecommendationLimits{PodMinCPUMillicores: 10, PodMinMemoryMb: 1},
		policyLister:  newVhapePolicyLister(policy),
		metricsClient: &mockMetricsClient{},
		estimators:    make(map[model.VpaID]*ResourceEstimators),
		startupHistory: map[model.PodID]*history.PodHistory{
			podID: {Samples: map[string][]model.ContainerUsageSample{
				"app": append(usageSamples(now.Add(-2*time.Hour), 300, 1000), usageSamples(now.Add(-time.Hour), 400, 1000)...),
			}},
		},
	}
	state := model.NewAggregateContainerState()
	state.ObserveRequest(model.Resources{model.ResourceCPU: 100, model.ResourceMemory: 1000}, now)
	state.UpdateFromPolicy(&vpa_types.ContainerResourcePolicy{
		ControlledResources: &[]v1.ResourceName{v1.ResourceCPU},
	})

	got, err := r.GetRecommendedPodResources(
		model.ContainerNameToAggregateStateMap{"app": state},
		vpa,
		[]model.PodID{podID},
	)

	assert.NoError(t, err)
	assert.Equal(t, model.ResourceAmount(400), got["app"].Target[model.ResourceCPU])
}
