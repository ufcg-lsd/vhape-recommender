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

func TestRobustMadEwmaFallsBackWithoutEnoughSamples(t *testing.T) {
	estimator := NewRobustMadEwmaEstimator(model.ResourceMemory, time.Hour, time.Minute)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250 * model.ResourceAmount(mib),
		Min:            100 * model.ResourceAmount(mib),
	})

	assert.Equal(t, 250*model.ResourceAmount(mib), got.Target)
	assert.Equal(t, got.Target, got.LowerBound)
	assert.Equal(t, got.Target, got.UpperBound)
	assert.Equal(t, got.Target, got.UncappedTarget)
}

func TestRobustMadEwmaFallsBackToMinimumWithoutCurrentRequest(t *testing.T) {
	estimator := NewRobustMadEwmaEstimator(model.ResourceMemory, time.Hour, time.Minute)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		Min: 100 * model.ResourceAmount(mib),
	})

	assert.Equal(t, 100*model.ResourceAmount(mib), got.Target)
}

func TestRobustMadEwmaIgnoresNonMemoryResource(t *testing.T) {
	estimator := NewRobustMadEwmaEstimator(model.ResourceCPU, time.Hour, time.Minute)
	now := time.Now()
	estimator.samples["app"] = []TimedSample{
		{Value: 500 * model.ResourceAmount(mib), Timestamp: now.Add(-2 * time.Minute)},
		{Value: 900 * model.ResourceAmount(mib), Timestamp: now},
	}

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250 * model.ResourceAmount(mib),
		Min:            100 * model.ResourceAmount(mib),
	})

	// Non-memory: leaves the request untouched.
	assert.Equal(t, 250*model.ResourceAmount(mib), got.Target)
}

func TestRobustMadEwmaIgnoresNegativeSamples(t *testing.T) {
	estimator := NewRobustMadEwmaEstimator(model.ResourceMemory, time.Hour, time.Minute)
	estimator.FeedSamples("app", []model.ResourceAmount{-1})

	assert.Empty(t, estimator.samples["app"])
}

func TestRobustMadEwmaPurgesExpiredSamples(t *testing.T) {
	estimator := NewRobustMadEwmaEstimator(model.ResourceMemory, time.Minute, 30*time.Second)
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

func TestRobustMadEwmaReactsToPersistentPeak(t *testing.T) {
	estimator := NewRobustMadEwmaEstimator(model.ResourceMemory, time.Hour, time.Minute)
	now := time.Now()

	samples := []TimedSample{}
	// A long stable baseline near 200Mi.
	for i := 0; i < 40; i++ {
		samples = append(samples, TimedSample{
			Value:     200 * model.ResourceAmount(mib),
			Timestamp: now.Add(-time.Duration(40-i) * time.Minute),
		})
	}
	// A persistent recent elevation near 500Mi.
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

	// The request should move up meaningfully in response to the sustained peak.
	assert.Greater(t, int64(got.Target), int64(220*model.ResourceAmount(mib)))
	// Bounds bracket the target.
	assert.Less(t, int64(got.LowerBound), int64(got.Target))
	assert.Greater(t, int64(got.UpperBound), int64(got.Target))
}

func TestRobustMadEwmaAppliesMaximumBound(t *testing.T) {
	estimator := NewRobustMadEwmaEstimator(model.ResourceMemory, time.Hour, time.Minute)
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
	// The uncapped target reflects the estimator output before clamping.
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

func TestRobustMadEwmaSpecBuildsEstimatorFromPolicyConfig(t *testing.T) {
	spec, name, err := BuildHeuristic(map[string]runtime.RawExtension{
		RobustMadEwma: {Raw: []byte(`{"slidingWindow":"24h","minimumCoverageWindow":"30m"}`)},
	})

	assert.NoError(t, err)
	assert.Equal(t, RobustMadEwma, name)
	assert.Equal(t, &RobustMadEwmaSpec{
		SlidingWindow:         metav1.Duration{Duration: 24 * time.Hour},
		MinimumCoverageWindow: metav1.Duration{Duration: 30 * time.Minute},
	}, spec)

	estimator, ok := spec.NewEstimator(model.ResourceMemory).(*RobustMadEwmaEstimator)

	assert.True(t, ok)
	assert.Equal(t, model.ResourceMemory, estimator.resourceName)
	assert.Equal(t, 24*time.Hour, estimator.slidingWindow)
	assert.Equal(t, 30*time.Minute, estimator.minimumCoverageWindow)
}

func TestRobustMadEwmaSpecRejectsMalformedParameters(t *testing.T) {
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
				RobustMadEwma: {Raw: []byte(tt.config)},
			})

			assert.ErrorContains(t, err, "invalid parameters")
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestMedianMeanMaxFloatHelpers(t *testing.T) {
	assert.Equal(t, 2.0, median([]float64{3, 1, 2}))
	assert.Equal(t, 2.5, median([]float64{1, 2, 3, 4}))
	assert.Equal(t, 2.0, mean([]float64{1, 2, 3}))
	assert.Equal(t, 4.0, maxFloat([]float64{1, 4, 2}))
}
