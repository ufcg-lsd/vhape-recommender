package estimators

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

// timedSamples builds samples spaced by the given interval, ending now.
func timedSamples(interval time.Duration, values ...model.ResourceAmount) []TimedSample {
	start := time.Now().Add(-interval * time.Duration(len(values)-1))
	samples := make([]TimedSample, 0, len(values))
	for i, value := range values {
		samples = append(samples, TimedSample{Value: value, Timestamp: start.Add(interval * time.Duration(i))})
	}
	return samples
}

func TestSMAIsRegistered(t *testing.T) {
	assert.Contains(t, heuristics, SMA)
}

func TestSMASpecBuildsEstimatorFromPolicyConfig(t *testing.T) {
	spec, name, err := BuildHeuristic(map[string]runtime.RawExtension{
		SMA: {Raw: []byte(`{"N":3,"input":2}`)},
	})

	assert.NoError(t, err)
	assert.Equal(t, SMA, name)
	assert.Equal(t, &SMASpec{N: 3, Input: 2}, spec)

	estimator, ok := spec.NewEstimator(model.ResourceCPU).(*SMAEstimator)

	assert.True(t, ok)
	assert.Equal(t, model.ResourceCPU, estimator.resourceName)
	assert.Equal(t, 3, estimator.n)
	assert.Equal(t, 2, estimator.input)
	assert.Equal(t, 15*time.Second, estimator.repeatedSampleInterval)
}

func TestSMASpecRejectsMalformedParameters(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "no parameters", config: ``, wantErr: "unexpected end of JSON input"},
		{name: "parameters are not an object", config: `"bad"`, wantErr: "cannot unmarshal string"},
		{name: "N is not an integer", config: `{"N":"three","input":2}`, wantErr: "cannot unmarshal string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := BuildHeuristic(map[string]runtime.RawExtension{SMA: {Raw: []byte(tt.config)}})

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSMAFallsBackWithoutSamples(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{
		CurrentRequest: 250,
		Min:            100,
	})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
	assert.Equal(t, got.Target, got.LowerBound)
	assert.Equal(t, got.Target, got.UpperBound)
	assert.Equal(t, got.Target, got.UncappedTarget)
}

func TestSMAFallsBackToMinimumWithoutCurrentRequest(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 100})

	assert.Equal(t, model.ResourceAmount(100), got.Target)
	assert.Equal(t, got.Target, got.UpperBound)
}

func TestSMAIgnoresNegativeSamples(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)
	estimator.FeedSamples("app", []model.ResourceAmount{-1})

	assert.Empty(t, estimator.samples["app"])
}

func TestPredictSMA(t *testing.T) {
	tests := []struct {
		name   string
		values []model.ResourceAmount
		n      int
		input  int
		want   float64
	}{
		// Fewer than n samples: plain average.
		{name: "fewer samples than one window", values: []model.ResourceAmount{100, 200}, n: 3, input: 2, want: 150},
		// Windows [200,300,400]=300 and [100,200,300]=200, trend 100:
		// max(1.05*300, 100*0.9*2+200) = 380.
		{name: "all windows, rising", values: []model.ResourceAmount{100, 200, 300, 400}, n: 3, input: 2, want: 380},
		// Windows 200 and 300, trend -100: max(1.05*200, -180+300) = 210.
		{name: "all windows, falling", values: []model.ResourceAmount{400, 300, 200, 100}, n: 3, input: 2, want: 210},
		// Only 2 of 3 windows fit: [200,300]=250 and [100,200]=150, trend 100:
		// max(1.05*250, 100*0.9*2+150) = 330.
		{name: "partial windows", values: []model.ResourceAmount{100, 200, 300}, n: 2, input: 3, want: 330},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := predictSMA(timedSamples(time.Second, tt.values...), tt.n, tt.input)

			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestPredictSMAKeepsDivisionByZeroWithExactlyNSamples(t *testing.T) {
	got := predictSMA(timedSamples(time.Second, 100, 200, 300), 3, 2)

	assert.True(t, math.IsNaN(got), "Tiny Autoscalers divides by zero with a single window; got %v", got)
}

func TestSMARecommendationFollowsTinyDecoratorChain(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)
	// Prediction = max(1.05*1000.667, 0.333*0.9*2+1000.333) = 1050.7.
	// base = 1050; target = 1050 + trunc(1050*0.15) = 1207 (a single float
	// computation would give 1208).
	// confidence = min(3h in days, 4/1440) = 1/360.
	// upper = trunc(1207 * (1+360)) = 435727; lower = trunc(1207 / 1.36^2) = 652.
	estimator.samples["app"] = timedSamples(time.Hour, 1000, 1000, 1001, 1001)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 25})

	assert.Equal(t, model.ResourceAmount(1207), got.Target)
	assert.Equal(t, model.ResourceAmount(1207), got.UncappedTarget)
	assert.Equal(t, model.ResourceAmount(435727), got.UpperBound)
	assert.Equal(t, model.ResourceAmount(652), got.LowerBound)
}

func TestSMACapsUpperBoundWithSingleSample(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)
	estimator.FeedSamples("app", []model.ResourceAmount{500})

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 25})

	// Zero confidence: infinite upper bound capped like the VPA, lower bound
	// scaled to zero and raised to the minimum.
	assert.Equal(t, model.ResourceAmount(575), got.Target)
	assert.Equal(t, model.MaxResourceAmount, got.UpperBound)
	assert.Equal(t, model.ResourceAmount(25), got.LowerBound)
}

func TestSMAAppliesMaximumBound(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)
	estimator.samples["app"] = timedSamples(time.Hour, 1000, 1000, 1001, 1001)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 25, Max: 1000})

	assert.Equal(t, model.ResourceAmount(1000), got.Target)
	assert.Equal(t, model.ResourceAmount(1207), got.UncappedTarget)
	assert.Equal(t, model.ResourceAmount(1000), got.UpperBound)
}

func TestSMADiscardsRepeatedSamples(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)
	for i := 0; i < 15; i++ {
		estimator.FeedSamples("app", []model.ResourceAmount{899})
	}

	assert.Len(t, estimator.samples["app"], 1)
}

func TestSMAAcceptsChangedSamples(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)
	estimator.FeedSamples("app", []model.ResourceAmount{899})
	estimator.FeedSamples("app", []model.ResourceAmount{902})
	estimator.FeedSamples("app", []model.ResourceAmount{899})

	assert.Len(t, estimator.samples["app"], 3)
}

func TestSMAAcceptsRepeatedValueAfterInterval(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, 50*time.Millisecond)
	estimator.FeedSamples("app", []model.ResourceAmount{0})
	estimator.FeedSamples("app", []model.ResourceAmount{0})
	time.Sleep(60 * time.Millisecond)
	estimator.FeedSamples("app", []model.ResourceAmount{0})

	assert.Len(t, estimator.samples["app"], 2)
}

func TestSMAIdleContainerReachesZero(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, 10*time.Millisecond)
	for _, value := range []model.ResourceAmount{1250, 1240, 0} {
		estimator.FeedSamples("app", []model.ResourceAmount{value})
	}
	for i := 0; i < 2; i++ {
		time.Sleep(15 * time.Millisecond)
		estimator.FeedSamples("app", []model.ResourceAmount{0})
	}

	assert.Zero(t, predictSMA(estimator.samples["app"], 3, 2))
}

func TestSMAFeedsMultipleContainers(t *testing.T) {
	estimator := NewSMAEstimator(model.ResourceCPU, 3, 2, time.Hour)
	estimator.FeedSamples("app", []model.ResourceAmount{500})
	estimator.FeedSamples("sidecar", []model.ResourceAmount{500})

	assert.Len(t, estimator.samples["app"], 1)
	assert.Len(t, estimator.samples["sidecar"], 1)
}

func TestResourceAmountFromFloat(t *testing.T) {
	assert.Equal(t, model.ResourceAmount(0), resourceAmountFromFloat(-1))
	assert.Equal(t, model.ResourceAmount(12), resourceAmountFromFloat(12.9))
	assert.Equal(t, model.MaxResourceAmount, resourceAmountFromFloat(math.Inf(1)))
}
