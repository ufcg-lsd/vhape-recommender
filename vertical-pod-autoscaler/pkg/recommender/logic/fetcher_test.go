package logic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	input_metrics "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/metrics"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/estimators"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

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

func TestFetchPolicyReturnsValidPolicy(t *testing.T) {
	lister := newVhapePolicyLister(newTestPolicyObject("my-policy", newTestPolicySpec()))

	r := &podResourceRecommender{policyLister: lister}
	policy, err := r.fetchPolicy(newTestVPA("default", "my-vpa", "my-policy"))

	assert.NoError(t, err)
	assert.NotNil(t, policy)
	assert.Equal(t, "my-policy", policy.Name)
	assert.Contains(t, policy.Spec.Resources.CPU.ScalingHeuristic, estimators.PercentileHysteresis)
}

func TestFetchPolicyReturnsErrorForInvalidAnnotation(t *testing.T) {
	r := &podResourceRecommender{}

	policy, err := r.fetchPolicy(newTestVPA("default", "my-vpa", ""))

	assert.Nil(t, policy)
	assert.ErrorContains(t, err, "missing vhape/policy annotation")
}

func TestFetchPolicyReturnsErrorWhenPolicyDoesNotExist(t *testing.T) {
	lister := newVhapePolicyLister()

	r := &podResourceRecommender{policyLister: lister}
	policy, err := r.fetchPolicy(newTestVPA("default", "my-vpa", "missing"))

	assert.Nil(t, policy)
	assert.Error(t, err)
	assert.ErrorContains(t, err, `fetch policy "missing"`)
}
