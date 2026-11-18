package logic

import (
	"context"
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

func cycleSnapshots(pods ...model.PodID) []*input_metrics.ContainerMetricsSnapshot {
	snapshots := make([]*input_metrics.ContainerMetricsSnapshot, len(pods))
	for i, podID := range pods {
		snapshots[i] = &input_metrics.ContainerMetricsSnapshot{
			ID:    model.ContainerID{PodID: podID, ContainerName: "app"},
			Usage: model.Resources{model.ResourceCPU: model.ResourceAmount(100 * (i + 1)), model.ResourceMemory: model.ResourceAmount(1000 * (i + 1))},
		}
	}
	return snapshots
}

func TestCollectCurrentUsageReadsTheSnapshotOfTheCycle(t *testing.T) {
	podA := model.PodID{Namespace: "default", PodName: "pod-a"}
	podB := model.PodID{Namespace: "default", PodName: "pod-b"}
	podC := model.PodID{Namespace: "other", PodName: "pod-c"}
	mc := &mockMetricsClient{snapshots: cycleSnapshots(podB, podC, podA)}
	r := &podResourceRecommender{metricsClient: mc}

	withoutSnapshot, err := r.collectCurrentUsage([]model.PodID{podA, podB})
	assert.NoError(t, err)

	r.RefreshUsage(context.Background())
	assert.Equal(t, 2, mc.calls)
	for range 3 {
		usage, err := r.collectCurrentUsage([]model.PodID{podA, podB})
		assert.NoError(t, err)
		// Same samples, in the order of the metrics list (pod-b before pod-a).
		assert.Equal(t, withoutSnapshot, usage)
		assert.Equal(t, []model.ResourceAmount{100, 300}, usage.CPU["app"])
	}
	assert.Equal(t, 2, mc.calls, "the VPAs of the cycle must not fetch the metrics again")

	r.ReleaseUsage()
	_, err = r.collectCurrentUsage([]model.PodID{podA})
	assert.NoError(t, err)
	assert.Equal(t, 3, mc.calls, "without the snapshot, each VPA fetches the metrics")
}

func TestCollectCurrentUsageFetchesOnItsOwnWhenTheCycleFetchFails(t *testing.T) {
	podA := model.PodID{Namespace: "default", PodName: "pod-a"}
	mc := &mockMetricsClient{err: assert.AnError}
	r := &podResourceRecommender{metricsClient: mc}

	r.RefreshUsage(context.Background())
	assert.Nil(t, r.usage)

	mc.err = nil
	mc.snapshots = cycleSnapshots(podA)
	usage, err := r.collectCurrentUsage([]model.PodID{podA})
	assert.NoError(t, err)
	assert.Equal(t, []model.ResourceAmount{100}, usage.CPU["app"])
	assert.Equal(t, 2, mc.calls)
}

func TestCollectCurrentUsageKeepsAnEmptySnapshot(t *testing.T) {
	mc := &mockMetricsClient{}
	r := &podResourceRecommender{metricsClient: mc}

	r.RefreshUsage(context.Background())
	usage, err := r.collectCurrentUsage([]model.PodID{{Namespace: "default", PodName: "pod-a"}})
	assert.NoError(t, err)
	assert.Empty(t, usage.CPU)
	assert.Equal(t, 1, mc.calls, "an empty snapshot is still the snapshot of the cycle")
}
