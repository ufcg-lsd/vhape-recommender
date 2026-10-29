package estimators

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/klog/v2"
)

// SMA is the name used to select this heuristic in a VhapePolicy.
const SMA = "sma"

// The constants below reproduce values that are fixed in the Tiny Autoscalers
// serverless-recommender (kubernetes/autoscaler@b03f204804c9 plus
// On-Demand-Resizing), so they are deliberately not configurable.
const (
	// recommendationMarginFraction is the default --recommendation-margin-fraction
	// of the VPA recommender, applied to every estimation by WithMargin.
	recommendationMarginFraction = 0.15

	// Confidence multipliers applied by WithConfidenceMultiplier to the upper
	// and lower bounds; the target receives no multiplier.
	upperBoundConfidenceMultiplier = 1.0
	upperBoundConfidenceExponent   = 1.0
	lowerBoundConfidenceMultiplier = 0.001
	lowerBoundConfidenceExponent   = -2.0

	// repeatedSampleInterval is the minimum time after the last accepted batch
	// before an identical batch is accepted again. It must match the
	// metrics-server --metric-resolution: 15s is the smallest practical value
	// for current metrics-server releases and the refresh cadence visible in
	// the Tiny Autoscalers paper. With the metrics-server default of 60s, a
	// value that genuinely repeats (e.g. an idle container at 0) is accepted
	// up to four times per real reading.
	repeatedSampleInterval = 15 * time.Second
)

func init() {
	RegisterHeuristic(SMA, func(config []byte) (HeuristicSpec, error) {
		spec := &SMASpec{}
		if err := json.Unmarshal(config, spec); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		return spec, nil
	})
}

// SMASpec contains the configuration for the simple moving average heuristic.
//
// The fields map one to one to Predict_SMA(N, input) from Tiny Autoscalers:
// N is the number of samples averaged by each window and Input is the number
// of windows used to extrapolate the trend.
type SMASpec struct {
	N     int `json:"N"`
	Input int `json:"input"`
}

// NewEstimator creates a simple moving average estimator for the given resource.
func (s *SMASpec) NewEstimator(resourceName model.ResourceName) ResourceEstimator {
	return NewSMAEstimator(resourceName, s.N, s.Input, repeatedSampleInterval)
}

// SMAEstimator reproduces the SMA-based recommender from Tiny Autoscalers.
//
// The target is a simple moving average prediction plus the VPA safety margin,
// and the bounds are widened by the VPA confidence multiplier, which depends on
// how much history has been collected.
//
// As in the original, samples are kept per container in an unbounded buffer:
// nothing is purged by age and entries of removed containers are never
// released. This is intentional, to keep the heuristic faithful for
// comparison purposes.
type SMAEstimator struct {
	mu                     sync.Mutex
	resourceName           model.ResourceName
	n                      int
	input                  int
	repeatedSampleInterval time.Duration
	samples                map[string][]TimedSample
	lastBatch              map[string][]model.ResourceAmount
	lastAccepted           map[string]time.Time
}

func NewSMAEstimator(
	resourceName model.ResourceName,
	n int,
	input int,
	repeatedSampleInterval time.Duration,
) *SMAEstimator {

	return &SMAEstimator{
		resourceName:           resourceName,
		n:                      n,
		input:                  input,
		repeatedSampleInterval: repeatedSampleInterval,
		samples:                make(map[string][]TimedSample),
		lastBatch:              make(map[string][]model.ResourceAmount),
		lastAccepted:           make(map[string]time.Time),
	}
}

// FeedSamples stores new usage samples for the given container.
//
// The recommender delivers the latest metrics-server reading on every cycle,
// so when the recommender interval is shorter than the metric resolution the
// same reading arrives several times. The VPA recommender used by Tiny
// Autoscalers discards those by the sample timestamp (model/container.go,
// addCPUSample); estimators only receive values, so a batch identical to the
// last accepted one is discarded instead, unless repeatedSampleInterval has
// elapsed and it is therefore a new reading that happens to repeat the value.
func (e *SMAEstimator) FeedSamples(containerName string, samples []model.ResourceAmount) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	if slices.Equal(samples, e.lastBatch[containerName]) &&
		now.Sub(e.lastAccepted[containerName]) < e.repeatedSampleInterval {
		klog.V(5).InfoS(
			"SMA: repeated samples discarded",
			"resource", e.resourceName,
			"containerName", containerName,
			"received", len(samples),
		)
		return
	}
	e.lastBatch[containerName] = slices.Clone(samples)
	e.lastAccepted[containerName] = now

	inserted := 0
	for _, sample := range samples {
		if sample < 0 {
			continue
		}

		e.samples[containerName] = append(e.samples[containerName], TimedSample{
			Value:     sample,
			Timestamp: now,
		})

		inserted++
	}

	klog.V(4).InfoS(
		"SMA: samples inserted",
		"resource", e.resourceName,
		"containerName", containerName,
		"received", len(samples),
		"inserted", inserted,
		"totalCurrentSamples", len(e.samples[containerName]),
	)
}

// GetSingleResourceRecommendation returns a recommendation for the configured
// resource and the given container.
//
// The computation follows the decorator chain of the Tiny Autoscalers
// recommender, including its integer conversions: the prediction is converted
// to a resource amount, the margin is added to it, and the confidence
// multiplier scales the result for the bounds. Constraints are applied last,
// like WithMinResources in the original.
//
// If no samples are available, the recommendation falls back to
// constraints.CurrentRequest. If CurrentRequest is not set, constraints.Min is
// used instead.
func (e *SMAEstimator) GetSingleResourceRecommendation(containerName string, constraints ContainerResourceConstraints) recommendation.SingleResourceRecommendation {
	e.mu.Lock()
	defer e.mu.Unlock()

	samples := e.samples[containerName]
	if len(samples) == 0 {
		klog.V(4).InfoS("SMA: no samples",
			"resource", e.resourceName,
			"containerName", containerName,
		)

		fallback := constraints.CurrentRequest
		if fallback <= 0 {
			fallback = constraints.Min
		}

		return recommendation.SingleResourceRecommendation{
			Target:         fallback,
			LowerBound:     fallback,
			UpperBound:     fallback,
			UncappedTarget: fallback,
		}
	}

	base := resourceAmountFromFloat(predictSMA(samples, e.n, e.input))
	uncappedTarget := base + model.ScaleResource(base, recommendationMarginFraction)

	conf := confidence(samples)
	lowerBound := model.ScaleResource(uncappedTarget,
		math.Pow(1+lowerBoundConfidenceMultiplier/conf, lowerBoundConfidenceExponent))
	upperBound := model.ScaleResource(uncappedTarget,
		math.Pow(1+upperBoundConfidenceMultiplier/conf, upperBoundConfidenceExponent))

	target := applyConstraints(uncappedTarget, constraints)
	lowerBound = applyConstraints(lowerBound, constraints)
	upperBound = applyConstraints(upperBound, constraints)

	klog.V(4).InfoS(
		"SMA: recommendation",
		"resource", e.resourceName,
		"containerName", containerName,
		"samples", len(samples),
		"base", base,
		"confidence", conf,
		"uncappedTarget", uncappedTarget,
		"target", target,
		"lowerBound", lowerBound,
		"upperBound", upperBound,
	)

	return recommendation.SingleResourceRecommendation{
		Target:         target,
		LowerBound:     lowerBound,
		UpperBound:     upperBound,
		UncappedTarget: uncappedTarget,
	}
}

// confidence reproduces getConfidence from the VPA recommender: a measure of
// the available history, equal to N for N days of samples at one sample per
// minute. Like the original, it is zero for a single sample, which makes the
// upper bound infinite (capped to model.MaxResourceAmount).
func confidence(samples []TimedSample) float64 {
	lifespanInDays := samples[len(samples)-1].Timestamp.Sub(samples[0].Timestamp).Hours() / 24
	samplesAmount := float64(len(samples)) / (60 * 24)

	return math.Min(lifespanInDays, samplesAmount)
}

// predictSMA reproduces Predict_SMA from Tiny Autoscalers (util/observation.go).
//
// It averages the last n samples over up to input windows, each shifted by one
// sample, and extrapolates the trend between the newest and the oldest window.
// With fewer than n samples it returns the plain average of all samples.
//
// When exactly n samples are available there is a single window and the trend
// divides by zero, producing NaN. The original has the same behavior, which is
// kept on purpose.
func predictSMA(samples []TimedSample, n, input int) float64 {
	length := len(samples)
	if length < n {
		return averageSamples(samples)
	}

	windows := input
	if length < input-1+n {
		windows = length - n + 1
	}

	averages := make([]float64, 0, windows)
	for i := 0; i < windows; i++ {
		averages = append(averages, averageSamples(samples[length-i-n:length-i]))
	}

	newest, oldest := averages[0], averages[len(averages)-1]
	trend := (newest - oldest) / float64(len(averages)-1)
	recent := averageSamples(samples[length-n:])

	return math.Max(1.05*recent, trend*0.9*float64(len(averages))+oldest)
}

// averageSamples returns the mean value of the given samples, or zero when
// there are none.
func averageSamples(samples []TimedSample) float64 {
	if len(samples) == 0 {
		return 0
	}

	sum := 0.0
	for _, sample := range samples {
		sum += float64(sample.Value)
	}

	return sum / float64(len(samples))
}

// resourceAmountFromFloat converts a float to a resource amount the same way as
// the unexported model.resourceAmountFromFloat used by the VPA recommender:
// negative values become zero and values above model.MaxResourceAmount are
// capped. NaN is converted without checks, as in the original.
func resourceAmountFromFloat(amount float64) model.ResourceAmount {
	if amount < 0 {
		return 0
	}
	if amount > float64(model.MaxResourceAmount) {
		return model.MaxResourceAmount
	}

	return model.ResourceAmount(amount)
}
