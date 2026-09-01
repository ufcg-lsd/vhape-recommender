package estimators

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestPercentileHysteresisFallsBackWithoutEnoughSamples(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.9, 0.1, time.Hour, time.Minute)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250,
		Min:            100,
	})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
	assert.Equal(t, got.Target, got.LowerBound)
	assert.Equal(t, got.Target, got.UpperBound)
	assert.Equal(t, got.Target, got.UncappedTarget)
}

func TestPercentileHysteresisFallsBackToMinimumWithoutCurrentRequest(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.9, 0.1, time.Hour, time.Minute)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		Min: 100,
	})

	assert.Equal(t, model.ResourceAmount(100), got.Target)
	assert.Equal(t, got.Target, got.LowerBound)
	assert.Equal(t, got.Target, got.UpperBound)
	assert.Equal(t, got.Target, got.UncappedTarget)
}

func TestPercentileHysteresisIgnoresNegativeSamples(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.5, 0, time.Hour, time.Minute)
	estimator.FeedSamples("app", []model.ResourceAmount{-1})

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 200,
		Min:            100,
	})

	assert.Equal(t, model.ResourceAmount(200), got.Target)
	assert.Empty(t, estimator.samples["app"])
}

func TestPercentileHysteresisUsesPercentileAndHeadroom(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.5, 0.5, time.Hour, time.Minute)
	estimator.samples["app"] = []TimedSample{
		{Value: 100, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Value: 200, Timestamp: time.Now().Add(-time.Minute)},
		{Value: 300, Timestamp: time.Now()},
	}

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		Min: 100,
	})

	assert.Equal(t, model.ResourceAmount(300), got.Target)
	assert.Equal(t, model.ResourceAmount(270), got.LowerBound)
	assert.Equal(t, model.ResourceAmount(330), got.UpperBound)
	assert.Equal(t, model.ResourceAmount(300), got.UncappedTarget)
}

func TestPercentileHysteresisAppliesMaximumBound(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.5, 0.5, time.Hour, time.Minute)
	estimator.samples["app"] = []TimedSample{
		{Value: 100, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Value: 200, Timestamp: time.Now().Add(-time.Minute)},
		{Value: 300, Timestamp: time.Now()},
	}

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		Min: 100,
		Max: 250,
	})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
	assert.Equal(t, model.ResourceAmount(225), got.LowerBound)
	assert.Equal(t, model.ResourceAmount(275), got.UpperBound)
	assert.Equal(t, model.ResourceAmount(300), got.UncappedTarget)
}

func TestPercentileHysteresisPurgesExpiredSamples(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.5, 0, time.Minute, 30*time.Second)
	estimator.samples["app"] = []TimedSample{
		{Value: 100, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Value: 200, Timestamp: time.Now()},
	}

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250,
		Min:            100,
	})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
	assert.Len(t, estimator.samples["app"], 1)
}

func TestApplyConstraints(t *testing.T) {
	assert.Equal(t, model.ResourceAmount(100), applyConstraints(50, ContainerResourceConstraints{Min: 100, Max: 200}))
	assert.Equal(t, model.ResourceAmount(200), applyConstraints(250, ContainerResourceConstraints{Min: 100, Max: 200}))
	assert.Equal(t, model.ResourceAmount(250), applyConstraints(250, ContainerResourceConstraints{Min: 100}))
}

func TestPercentileHysteresisFallsBackWithSingleSample(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.9, 0.1, time.Hour, time.Minute)
	estimator.FeedSamples("app", []model.ResourceAmount{100})

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250,
		Min:            100,
	})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
	assert.Equal(t, got.Target, got.LowerBound)
	assert.Equal(t, got.Target, got.UpperBound)
	assert.Equal(t, got.Target, got.UncappedTarget)
}

func TestPercentileHysteresisFallsBackWhenCoverageBelowThreshold(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.9, 0.1, time.Hour, 2*time.Second)
	now := time.Now()
	estimator.samples["app"] = []TimedSample{
		{Value: 100, Timestamp: now},
		{Value: 200, Timestamp: now.Add(1 * time.Second)},
	}

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250,
		Min:            100,
	})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
}

func TestPercentileHysteresisWindowCoverage(t *testing.T) {
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.9, 0.1, time.Hour, time.Minute)
	now := time.Now()

	assert.Zero(t, estimator.windowCoverage("app"))

	estimator.samples["app"] = []TimedSample{
		{Value: 100, Timestamp: now.Add(-3 * time.Minute)},
		{Value: 200, Timestamp: now},
	}

	assert.Equal(t, 3*time.Minute, estimator.windowCoverage("app"))
}

func TestPercentileHysteresisSucceedsExactlyAtThreshold(t *testing.T) {
	requiredCoverage := time.Minute
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.9, 0.0, time.Hour, requiredCoverage)
	now := time.Now()
	estimator.samples["app"] = []TimedSample{
		{Value: 100, Timestamp: now.Add(-requiredCoverage)},
		{Value: 200, Timestamp: now},
	}

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		Min: 100,
	})

	// 90th percentile of [100, 200] = 200, no headroom
	// Key assertion: recommendation is NOT the fallback (100).
	assert.Greater(t, int(got.Target), 100)
	assert.Equal(t, model.ResourceAmount(200), got.Target)
}

func TestScaleResourceAmount(t *testing.T) {
	assert.Equal(t, model.ResourceAmount(100), scaleResourceAmount(100, 1.0))
	assert.Equal(t, model.ResourceAmount(50), scaleResourceAmount(100, 0.5))
	assert.Equal(t, model.ResourceAmount(0), scaleResourceAmount(0, 1.5))
	assert.Equal(t, model.ResourceAmount(1), scaleResourceAmount(1, 1.0))
	assert.Equal(t, model.ResourceAmount(200), scaleResourceAmount(100, 2.0))
}

func TestPercentileHysteresisFeedsMultipleContainers(t *testing.T) {
	requiredCoverage := time.Minute
	estimator := NewPercentileHysteresisEstimator(model.ResourceCPU, 0.5, 0, time.Hour, requiredCoverage)
	now := time.Now()

	// 3 samples: 50th percentile index = ceil(0.5*3)-1 = 1 (middle value)
	estimator.samples["container-a"] = []TimedSample{
		{Value: 100, Timestamp: now.Add(-requiredCoverage)},
		{Value: 200, Timestamp: now.Add(-requiredCoverage / 2)},
		{Value: 300, Timestamp: now},
	}
	estimator.samples["container-b"] = []TimedSample{
		{Value: 300, Timestamp: now.Add(-requiredCoverage)},
		{Value: 400, Timestamp: now.Add(-requiredCoverage / 2)},
		{Value: 500, Timestamp: now},
	}

	gotA := estimator.GetSingleResourceRecommendation("container-a", ContainerResourceConstraints{Min: 1})
	gotB := estimator.GetSingleResourceRecommendation("container-b", ContainerResourceConstraints{Min: 1})

	// Container A: 50th percentile of [100, 200, 300] = 200
	assert.Equal(t, model.ResourceAmount(200), gotA.Target)
	// Container B: 50th percentile of [300, 400, 500] = 400
	assert.Equal(t, model.ResourceAmount(400), gotB.Target)
	// Verify they're tracked independently
	assert.NotEqual(t, gotA.Target, gotB.Target)
}

func TestPercentileHysteresisSpecBuildsEstimatorFromPolicyConfig(t *testing.T) {
	spec, name, err := BuildHeuristic(map[string]runtime.RawExtension{
		PercentileHysteresis: {Raw: []byte(`{"percentile":0.9,"headroom":0.15,"slidingWindow":"5m","minimumCoverageWindow":"1m"}`)},
	})

	assert.NoError(t, err)
	assert.Equal(t, PercentileHysteresis, name)
	assert.Equal(t, &PercentileHysteresisSpec{
		Percentile:            0.9,
		Headroom:              0.15,
		SlidingWindow:         metav1.Duration{Duration: 5 * time.Minute},
		MinimumCoverageWindow: metav1.Duration{Duration: time.Minute},
	}, spec)

	estimator, ok := spec.NewEstimator(model.ResourceMemory).(*PercentileHysteresisEstimator)

	assert.True(t, ok)
	assert.Equal(t, model.ResourceMemory, estimator.resourceName)
	assert.Equal(t, 0.9, estimator.percentile)
	assert.Equal(t, 0.15, estimator.headroom)
	assert.Equal(t, 5*time.Minute, estimator.slidingWindow)
	assert.Equal(t, time.Minute, estimator.minimumCoverageWindow)
}

func TestPercentileHysteresisSpecRejectsMalformedParameters(t *testing.T) {
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
			config:  `{"percentile":0.9,"headroom":0.15,"slidingWindow":"soon"}`,
			wantErr: `invalid duration "soon"`,
		},
		{
			name:    "minimum coverage window equals sliding window",
			config:  `{"percentile":0.9,"headroom":0.15,"slidingWindow":"5m","minimumCoverageWindow":"5m"}`,
			wantErr: "minimumCoverageWindow must be less than slidingWindow",
		},
		{
			name:    "minimum coverage window exceeds sliding window",
			config:  `{"percentile":0.9,"headroom":0.15,"slidingWindow":"5m","minimumCoverageWindow":"6m"}`,
			wantErr: "minimumCoverageWindow must be less than slidingWindow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := BuildHeuristic(map[string]runtime.RawExtension{
				PercentileHysteresis: {Raw: []byte(tt.config)},
			})

			assert.ErrorContains(t, err, "invalid parameters")
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
