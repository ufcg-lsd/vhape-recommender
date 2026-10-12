package estimators

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const mib = int64(bytesPerMiB)

func TestAdrsMemV2FallsBackWithoutEnoughSamples(t *testing.T) {
	estimator := NewAdrsMemV2Estimator(model.ResourceMemory, time.Hour, time.Minute)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250 * model.ResourceAmount(mib),
		Min:            100 * model.ResourceAmount(mib),
	})

	assert.Equal(t, 250*model.ResourceAmount(mib), got.Target)
	assert.Equal(t, got.Target, got.LowerBound)
	assert.Equal(t, got.Target, got.UpperBound)
	assert.Equal(t, got.Target, got.UncappedTarget)
}

func TestAdrsMemV2FallsBackToMinimumWithoutCurrentRequest(t *testing.T) {
	estimator := NewAdrsMemV2Estimator(model.ResourceMemory, time.Hour, time.Minute)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		Min: 100 * model.ResourceAmount(mib),
	})

	assert.Equal(t, 100*model.ResourceAmount(mib), got.Target)
}

func TestAdrsMemV2IgnoresNonMemoryResource(t *testing.T) {
	estimator := NewAdrsMemV2Estimator(model.ResourceCPU, time.Hour, time.Minute)
	now := time.Now()
	estimator.samples["app"] = []TimedSample{
		{Value: 500 * model.ResourceAmount(mib), Timestamp: now.Add(-2 * time.Minute)},
		{Value: 900 * model.ResourceAmount(mib), Timestamp: now},
	}

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250 * model.ResourceAmount(mib),
		Min:            100 * model.ResourceAmount(mib),
	})

	assert.Equal(t, 250*model.ResourceAmount(mib), got.Target)
}

func TestAdrsMemV2IgnoresNegativeSamples(t *testing.T) {
	estimator := NewAdrsMemV2Estimator(model.ResourceMemory, time.Hour, time.Minute)
	estimator.FeedSamples("app", []model.ResourceAmount{-1})

	assert.Empty(t, estimator.samples["app"])
}

func TestAdrsMemV2PurgesExpiredSamples(t *testing.T) {
	estimator := NewAdrsMemV2Estimator(model.ResourceMemory, time.Minute, 30*time.Second)
	estimator.samples["app"] = []TimedSample{
		{Value: 100 * model.ResourceAmount(mib), Timestamp: time.Now().Add(-2 * time.Minute)},
		{Value: 200 * model.ResourceAmount(mib), Timestamp: time.Now()},
	}

	estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250 * model.ResourceAmount(mib),
		Min:            100 * model.ResourceAmount(mib),
	})

	assert.Len(t, estimator.samples["app"], 1)
}

func TestAdrsMemV2ReactsToPersistentPeak(t *testing.T) {
	estimator := NewAdrsMemV2Estimator(model.ResourceMemory, time.Hour, time.Minute)
	now := time.Now()

	samples := []TimedSample{}
	for i := 0; i < 40; i++ {
		samples = append(samples, TimedSample{
			Value:     200 * model.ResourceAmount(mib),
			Timestamp: now.Add(-time.Duration(40-i) * time.Minute),
		})
	}
	for i := 0; i < 8; i++ {
		samples = append(samples, TimedSample{
			Value:     500 * model.ResourceAmount(mib),
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	estimator.samples["app"] = samples

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 200 * model.ResourceAmount(mib),
		Min:            50 * model.ResourceAmount(mib),
	})

	assert.Greater(t, int64(got.Target), int64(220*model.ResourceAmount(mib)))
	assert.Less(t, int64(got.LowerBound), int64(got.Target))
	assert.Greater(t, int64(got.UpperBound), int64(got.Target))
}

func TestAdrsMemV2AppliesMaximumBound(t *testing.T) {
	estimator := NewAdrsMemV2Estimator(model.ResourceMemory, time.Hour, time.Minute)
	now := time.Now()
	samples := []TimedSample{}
	for i := 0; i < 20; i++ {
		samples = append(samples, TimedSample{
			Value:     800 * model.ResourceAmount(mib),
			Timestamp: now.Add(-time.Duration(20-i) * time.Minute),
		})
	}
	estimator.samples["app"] = samples

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 400 * model.ResourceAmount(mib),
		Min:            100 * model.ResourceAmount(mib),
		Max:            500 * model.ResourceAmount(mib),
	})

	assert.Equal(t, 500*model.ResourceAmount(mib), got.Target)
	assert.Greater(t, int64(got.UncappedTarget), int64(got.Target))
}

func TestRecommendMemoryEmptyUsage(t *testing.T) {
	assert.Equal(t, 128.0*bytesPerMiB, recommendMemory(nil, 128.0*bytesPerMiB))
	assert.Equal(t, 32.0*bytesPerMiB, recommendMemory(nil, 0))
}

func TestRecommendMemoryHysteresisKeepsPrevOnTinyChange(t *testing.T) {
	usage := make([]float64, 50)
	for i := range usage {
		usage[i] = 200.0 * bytesPerMiB
	}
	prev := 205.0 * bytesPerMiB

	// A stable baseline close to the current request should not trigger an update.
	assert.Equal(t, prev, recommendMemory(usage, prev))
}

// TestAdrsMemV2RecoversAfterTransientSpike exercises the v2 behaviour: a single
// transient spike should not pin the request near its top. Starting from a
// request inflated by a past spike, over a baseline window the recommendation
// should decay back toward the real usage level.
func TestAdrsMemV2RecoversAfterTransientSpike(t *testing.T) {
	// Steady baseline around 200Mi with a single isolated spike to 900Mi.
	usage := make([]float64, 100)
	for i := range usage {
		usage[i] = 200.0 * bytesPerMiB
	}
	usage[10] = 900.0 * bytesPerMiB

	prev := 700.0 * bytesPerMiB // request inflated by a previous peak
	got := recommendMemory(usage, prev)

	// v2 should move the request down (faster decay + P95 floor), not keep it pinned.
	assert.Less(t, got, prev)
	assert.Greater(t, got, 200.0*bytesPerMiB) // but still safely above baseline
}

func TestAdrsMemV2SpecBuildsEstimatorFromPolicyConfig(t *testing.T) {
	spec, name, err := BuildHeuristic(map[string]runtime.RawExtension{
		AdrsMemV2: {Raw: []byte(`{"slidingWindow":"24h","minimumCoverageWindow":"30m"}`)},
	})

	assert.NoError(t, err)
	assert.Equal(t, AdrsMemV2, name)
	assert.Equal(t, &AdrsMemV2Spec{
		SlidingWindow:         metav1.Duration{Duration: 24 * time.Hour},
		MinimumCoverageWindow: metav1.Duration{Duration: 30 * time.Minute},
	}, spec)

	estimator, ok := spec.NewEstimator(model.ResourceMemory).(*AdrsMemV2Estimator)

	assert.True(t, ok)
	assert.Equal(t, model.ResourceMemory, estimator.resourceName)
	assert.Equal(t, 24*time.Hour, estimator.slidingWindow)
	assert.Equal(t, 30*time.Minute, estimator.minimumCoverageWindow)
}

func TestAdrsMemV2SpecRejectsMalformedParameters(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "no parameters",
			config:  ``,
			wantErr: "unexpected end of JSON input",
		},
		{
			name:    "parameters are not an object",
			config:  `"bad"`,
			wantErr: "cannot unmarshal string",
		},
		{
			name:    "sliding window is not a duration",
			config:  `{"slidingWindow":"soon","minimumCoverageWindow":"1m"}`,
			wantErr: `invalid duration "soon"`,
		},
		{
			name:    "minimum coverage window equals sliding window",
			config:  `{"slidingWindow":"5m","minimumCoverageWindow":"5m"}`,
			wantErr: "minimumCoverageWindow must be less than slidingWindow",
		},
		{
			name:    "minimum coverage window exceeds sliding window",
			config:  `{"slidingWindow":"5m","minimumCoverageWindow":"6m"}`,
			wantErr: "minimumCoverageWindow must be less than slidingWindow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := BuildHeuristic(map[string]runtime.RawExtension{
				AdrsMemV2: {Raw: []byte(tt.config)},
			})

			assert.ErrorContains(t, err, "invalid parameters")
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestStatHelpers(t *testing.T) {
	assert.Equal(t, 2.0, median([]float64{3, 1, 2}))
	assert.Equal(t, 2.5, median([]float64{1, 2, 3, 4}))
	assert.Equal(t, 2.0, mean([]float64{1, 2, 3}))
	assert.Equal(t, 4.0, maxFloat([]float64{1, 4, 2}))

	// numpy.percentile([1,2,3,4], 95) == 3.85 (linear interpolation)
	assert.InDelta(t, 3.85, percentile([]float64{1, 2, 3, 4}, 95), 1e-9)
	assert.Equal(t, 2.0, percentile([]float64{1, 2, 3}, 50))
	assert.Equal(t, 5.0, percentile([]float64{5}, 99))
}
