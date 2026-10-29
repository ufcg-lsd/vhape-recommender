package estimators

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

var vpaTestStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// vpaReference feeds the same readings to the upstream VPA model: one
// ContainerState per container instance, aggregated into an
// AggregateContainerState configured with the VPA defaults.
type vpaReference struct {
	aggregate *model.AggregateContainerState
	instances []*model.ContainerState
}

func newVPAReference(instances int) *vpaReference {
	ref := &vpaReference{aggregate: model.NewAggregateContainerState()}
	for i := 0; i < instances; i++ {
		ref.instances = append(ref.instances, model.NewContainerState(nil, ref.aggregate))
	}
	return ref
}

func (r *vpaReference) add(resource model.ResourceName, ts time.Time, usages ...model.ResourceAmount) {
	for i, usage := range usages {
		r.instances[i].AddSample(&model.ContainerUsageSample{MeasureStart: ts, Usage: usage, Resource: resource})
	}
}

// expected reproduces CreatePodResourceRecommender from the upstream VPA on top
// of the reference aggregate state.
func (r *vpaReference) expected(resource model.ResourceName, minimum model.ResourceAmount) (target, lower, upper model.ResourceAmount) {
	histogram := r.aggregate.AggregateCPUUsage
	toAmount := model.CPUAmountFromCores
	if resource == model.ResourceMemory {
		histogram = r.aggregate.AggregateMemoryPeaks
		toAmount = model.MemoryAmountFromBytes
	}
	withMargin := func(p float64) model.ResourceAmount {
		base := toAmount(histogram.Percentile(p))
		return base + model.ScaleResource(base, 0.15)
	}
	interval := 24 * time.Hour
	lifespan := float64(r.aggregate.LastSampleStart.Sub(r.aggregate.FirstSampleStart)) / float64(interval)
	confidence := math.Min(lifespan, float64(r.aggregate.TotalSamplesCount)/interval.Minutes())

	target = model.ResourceAmountMax(withMargin(0.9), minimum)
	lower = model.ResourceAmountMax(model.ScaleResource(withMargin(0.5), math.Pow(1+0.001/confidence, -2)), minimum)
	upper = model.ResourceAmountMax(model.ScaleResource(withMargin(0.95), math.Pow(1+1/confidence, 1)), minimum)
	return target, lower, upper
}

func newDefaultVPAEstimator(resource model.ResourceName) *VPAEstimator {
	return NewVPAEstimator(resource, *defaultVPASpec(), 15*time.Second)
}

func TestVPAIsRegistered(t *testing.T) {
	assert.Contains(t, heuristics, VPA)
}

func TestVPASpecDefaultsToUpstreamFlags(t *testing.T) {
	spec, name, err := BuildHeuristic(map[string]runtime.RawExtension{VPA: {Raw: []byte(`{}`)}})

	assert.NoError(t, err)
	assert.Equal(t, VPA, name)
	assert.Equal(t, &VPASpec{
		TargetPercentile:          0.9,
		LowerBoundPercentile:      0.5,
		UpperBoundPercentile:      0.95,
		SafetyMarginFraction:      0.15,
		ConfidenceInterval:        metav1.Duration{Duration: 24 * time.Hour},
		HistogramDecayHalfLife:    metav1.Duration{Duration: 24 * time.Hour},
		MemoryAggregationInterval: metav1.Duration{Duration: 24 * time.Hour},
	}, spec)
}

func TestVPASpecBuildsEstimatorFromPolicyConfig(t *testing.T) {
	spec, _, err := BuildHeuristic(map[string]runtime.RawExtension{
		VPA: {Raw: []byte(`{"targetPercentile":0.8,"safetyMarginFraction":0.2,"histogramDecayHalfLife":"1h","memoryAggregationInterval":"5m"}`)},
	})
	assert.NoError(t, err)

	estimator, ok := spec.NewEstimator(model.ResourceMemory).(*VPAEstimator)

	assert.True(t, ok)
	assert.Equal(t, model.ResourceMemory, estimator.resourceName)
	assert.Equal(t, 0.8, estimator.spec.TargetPercentile)
	assert.Equal(t, 0.5, estimator.spec.LowerBoundPercentile)
	assert.Equal(t, 0.2, estimator.spec.SafetyMarginFraction)
	assert.Equal(t, time.Hour, estimator.spec.HistogramDecayHalfLife.Duration)
	assert.Equal(t, 5*time.Minute, estimator.spec.MemoryAggregationInterval.Duration)
	assert.Equal(t, 15*time.Second, estimator.repeatedSampleInterval)
}

func TestVPASpecRejectsMalformedParameters(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "no parameters", config: ``, wantErr: "unexpected end of JSON input"},
		{name: "parameters are not an object", config: `"bad"`, wantErr: "cannot unmarshal string"},
		{name: "half-life is not a duration", config: `{"histogramDecayHalfLife":"soon"}`, wantErr: `invalid duration "soon"`},
		{name: "zero confidence interval", config: `{"confidenceInterval":"0s"}`, wantErr: "must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := BuildHeuristic(map[string]runtime.RawExtension{VPA: {Raw: []byte(tt.config)}})

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestVPAFallsBackWithoutSamples(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceCPU)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{CurrentRequest: 250, Min: 25})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
	assert.Equal(t, got.Target, got.LowerBound)
	assert.Equal(t, got.Target, got.UpperBound)
	assert.Equal(t, got.Target, got.UncappedTarget)
}

func TestVPAIgnoresNegativeSamples(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceCPU)
	estimator.FeedSamples("app", []model.ResourceAmount{-1})

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{CurrentRequest: 250, Min: 25})

	assert.Equal(t, model.ResourceAmount(250), got.Target)
}

func TestVPACPUMatchesUpstreamModel(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceCPU)
	ref := newVPAReference(1)

	// Six hours of a square wave sampled every 15s: 900m for 2 min, 100m for 2 min.
	for i := 0; i < 6*60*4; i++ {
		ts := vpaTestStart.Add(time.Duration(i) * 15 * time.Second)
		usage := model.ResourceAmount(100)
		if (i/8)%2 == 0 {
			usage = 900
		}
		estimator.feedSamplesAt("app", []model.ResourceAmount{usage}, ts)
		ref.add(model.ResourceCPU, ts, usage)
	}

	history := estimator.containers["app"]
	for _, p := range []float64{0.5, 0.9, 0.95} {
		assert.Equal(t, ref.aggregate.AggregateCPUUsage.Percentile(p), history.histogram.Percentile(p), "percentile %v", p)
	}
	assert.Equal(t, ref.aggregate.TotalSamplesCount, history.totalSamplesCount)
	assert.Equal(t, ref.aggregate.FirstSampleStart, history.firstSampleStart)
	assert.Equal(t, ref.aggregate.LastSampleStart, history.lastSampleStart)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 25})
	target, lower, upper := ref.expected(model.ResourceCPU, 25)

	assert.Equal(t, target, got.Target)
	assert.Equal(t, lower, got.LowerBound)
	assert.Equal(t, upper, got.UpperBound)
	assert.Equal(t, target, got.UncappedTarget)
}

func TestVPAMemoryPeaksMatchUpstreamModel(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceMemory)
	ref := newVPAReference(2)
	mib := model.ResourceAmount(1024 * 1024)

	// Three days of two pods, sampled hourly, with peaks rising and falling
	// within each 24h aggregation interval.
	for i := 0; i < 72; i++ {
		ts := vpaTestStart.Add(time.Duration(i) * time.Hour)
		podA := (200 + model.ResourceAmount((i*37)%150)) * mib
		podB := (300 + model.ResourceAmount((i*53)%90)) * mib
		estimator.feedSamplesAt("app", []model.ResourceAmount{podA, podB}, ts)
		ref.add(model.ResourceMemory, ts, podA, podB)
		// The VPA counts samples on the CPU histogram; feed the same timeline.
		ref.add(model.ResourceCPU, ts, 100, 100)
	}

	history := estimator.containers["app"]
	for _, p := range []float64{0.5, 0.9, 0.95} {
		assert.Equal(t, ref.aggregate.AggregateMemoryPeaks.Percentile(p), history.histogram.Percentile(p), "percentile %v", p)
	}

	minimum := model.ResourceAmount(250 * mib)
	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: minimum})
	target, lower, upper := ref.expected(model.ResourceMemory, minimum)

	assert.Equal(t, target, got.Target)
	assert.Equal(t, lower, got.LowerBound)
	assert.Equal(t, upper, got.UpperBound)
}

func TestVPACapsUpperBoundWithSingleSample(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceCPU)
	estimator.feedSamplesAt("app", []model.ResourceAmount{500}, vpaTestStart)

	got := estimator.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 25})

	// Zero confidence: infinite upper bound capped like the VPA, lower bound
	// scaled to zero and raised to the minimum.
	assert.Equal(t, model.MaxResourceAmount, got.UpperBound)
	assert.Equal(t, model.ResourceAmount(25), got.LowerBound)
}

func TestVPAUsesConfiguredPercentiles(t *testing.T) {
	spec := *defaultVPASpec()
	spec.TargetPercentile = 0.5
	median := NewVPAEstimator(model.ResourceCPU, spec, 15*time.Second)
	p90 := newDefaultVPAEstimator(model.ResourceCPU)

	for i, usage := range []model.ResourceAmount{100, 100, 100, 900, 900} {
		ts := vpaTestStart.Add(time.Duration(i) * time.Minute)
		median.feedSamplesAt("app", []model.ResourceAmount{usage}, ts)
		p90.feedSamplesAt("app", []model.ResourceAmount{usage}, ts)
	}

	low := median.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 25})
	high := p90.GetSingleResourceRecommendation("app", ContainerResourceConstraints{Min: 25})

	assert.Less(t, low.Target, high.Target)
}

func TestVPADiscardsRepeatedSamples(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceCPU)
	for i := 0; i < 15; i++ {
		estimator.feedSamplesAt("app", []model.ResourceAmount{899}, vpaTestStart.Add(time.Duration(i)*time.Second))
	}

	assert.Equal(t, 1, estimator.containers["app"].totalSamplesCount)
}

func TestVPAAcceptsRepeatedValueAfterInterval(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceCPU)
	estimator.feedSamplesAt("app", []model.ResourceAmount{0}, vpaTestStart)
	estimator.feedSamplesAt("app", []model.ResourceAmount{0}, vpaTestStart.Add(14*time.Second))
	estimator.feedSamplesAt("app", []model.ResourceAmount{0}, vpaTestStart.Add(15*time.Second))

	assert.Equal(t, 2, estimator.containers["app"].totalSamplesCount)
}

func TestVPAFeedsMultipleContainers(t *testing.T) {
	estimator := newDefaultVPAEstimator(model.ResourceCPU)
	estimator.feedSamplesAt("app", []model.ResourceAmount{500}, vpaTestStart)
	estimator.feedSamplesAt("sidecar", []model.ResourceAmount{500}, vpaTestStart)

	assert.Equal(t, 1, estimator.containers["app"].totalSamplesCount)
	assert.Equal(t, 1, estimator.containers["sidecar"].totalSamplesCount)
}
