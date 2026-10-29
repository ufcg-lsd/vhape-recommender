package estimators

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/util"
	"k8s.io/klog/v2"
)

// VPA is the name used to select this heuristic in a VhapePolicy.
const VPA = "vpa"

// The constants below are fixed in the upstream VPA recommender (1.6.0), so
// they are not configurable here either.
const (
	// Confidence multipliers applied to the upper and lower bounds by
	// CreatePodResourceRecommender (logic/recommender.go upstream).
	vpaUpperBoundConfidenceMultiplier = 1.0
	vpaUpperBoundConfidenceExponent   = 1.0
	vpaLowerBoundConfidenceMultiplier = 0.001
	vpaLowerBoundConfidenceExponent   = -2.0

	// vpaCPUSampleWeight is the weight of every CPU sample, the unexported
	// minSampleWeight of model/aggregations_config.go.
	vpaCPUSampleWeight = 0.1

	// vpaMemoryPeakWeight is the weight of every memory peak.
	vpaMemoryPeakWeight = 1.0

	// vpaRepeatedSampleInterval is the minimum time after the last accepted
	// batch before an identical batch is accepted again. The VPA discards a
	// repeated metrics-server reading by its timestamp; estimators only receive
	// values, so identical batches are discarded unless this interval, the
	// metrics-server --metric-resolution, has elapsed.
	vpaRepeatedSampleInterval = 15 * time.Second
)

func init() {
	RegisterHeuristic(VPA, func(config []byte) (HeuristicSpec, error) {
		spec := defaultVPASpec()
		if err := json.Unmarshal(config, spec); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if spec.ConfidenceInterval.Duration <= 0 || spec.HistogramDecayHalfLife.Duration <= 0 ||
			spec.MemoryAggregationInterval.Duration <= 0 {
			return nil, fmt.Errorf("invalid parameters: confidenceInterval, histogramDecayHalfLife and memoryAggregationInterval must be positive")
		}

		return spec, nil
	})
}

// VPASpec contains the configuration for the VPA heuristic.
//
// Every field mirrors a flag of the upstream VPA recommender and defaults to
// the flag default, so an empty configuration reproduces the stock VPA. In the
// VPA these flags are global; here they are set per resource.
type VPASpec struct {
	// TargetPercentile mirrors --target-cpu-percentile and --target-memory-percentile.
	TargetPercentile float64 `json:"targetPercentile"`
	// LowerBoundPercentile mirrors --recommendation-lower-bound-{cpu,memory}-percentile.
	LowerBoundPercentile float64 `json:"lowerBoundPercentile"`
	// UpperBoundPercentile mirrors --recommendation-upper-bound-{cpu,memory}-percentile.
	UpperBoundPercentile float64 `json:"upperBoundPercentile"`
	// SafetyMarginFraction mirrors --recommendation-margin-fraction.
	SafetyMarginFraction float64 `json:"safetyMarginFraction"`
	// ConfidenceInterval mirrors --confidence-interval-{cpu,memory}.
	ConfidenceInterval metav1.Duration `json:"confidenceInterval"`
	// HistogramDecayHalfLife mirrors --{cpu,memory}-histogram-decay-half-life.
	HistogramDecayHalfLife metav1.Duration `json:"histogramDecayHalfLife"`
	// MemoryAggregationInterval mirrors --memory-aggregation-interval. It is
	// only used for memory, which keeps one usage peak per interval.
	MemoryAggregationInterval metav1.Duration `json:"memoryAggregationInterval"`
}

func defaultVPASpec() *VPASpec {
	return &VPASpec{
		TargetPercentile:          0.9,
		LowerBoundPercentile:      0.5,
		UpperBoundPercentile:      0.95,
		SafetyMarginFraction:      0.15,
		ConfidenceInterval:        metav1.Duration{Duration: 24 * time.Hour},
		HistogramDecayHalfLife:    metav1.Duration{Duration: model.DefaultCPUHistogramDecayHalfLife},
		MemoryAggregationInterval: metav1.Duration{Duration: model.DefaultMemoryAggregationInterval},
	}
}

// NewEstimator creates a VPA estimator for the given resource.
func (s *VPASpec) NewEstimator(resourceName model.ResourceName) ResourceEstimator {
	return NewVPAEstimator(resourceName, *s, vpaRepeatedSampleInterval)
}

// VPAEstimator reproduces the recommendation algorithm of the upstream VPA
// recommender for one resource.
//
// Usage samples are added to a decaying histogram with the same bucket layout
// and decay as the VPA. For CPU every sample is added; for memory one peak is
// kept per aggregation interval and per container instance, as in
// model/container.go. The recommendation takes percentiles of the histogram,
// adds the safety margin and widens the bounds with the confidence multiplier,
// following CreatePodResourceRecommender.
//
// Differences from the VPA that come from the estimator interface: sample
// timestamps are the time FeedSamples is called, container instances are
// identified by their position in the samples slice, OOM events are not
// received, and history is kept in memory only (no checkpoints).
type VPAEstimator struct {
	mu                     sync.Mutex
	resourceName           model.ResourceName
	spec                   VPASpec
	histogramOptions       util.HistogramOptions
	repeatedSampleInterval time.Duration
	containers             map[string]*vpaContainerHistory
}

// vpaContainerHistory holds the aggregated usage of one container name, the
// equivalent of model.AggregateContainerState for a single resource.
type vpaContainerHistory struct {
	histogram         util.Histogram
	firstSampleStart  time.Time
	lastSampleStart   time.Time
	totalSamplesCount int
	memoryWindows     []vpaMemoryWindow
	lastBatch         []model.ResourceAmount
	lastAccepted      time.Time
}

// vpaMemoryWindow tracks the memory peak of one container instance in the
// current aggregation interval, as ContainerState does in the VPA.
type vpaMemoryWindow struct {
	windowEnd time.Time
	peak      model.ResourceAmount
}

func NewVPAEstimator(
	resourceName model.ResourceName,
	spec VPASpec,
	repeatedSampleInterval time.Duration,
) *VPAEstimator {

	halfLife := spec.HistogramDecayHalfLife.Duration
	config := model.NewAggregationsConfig(
		spec.MemoryAggregationInterval.Duration,
		model.DefaultMemoryAggregationIntervalCount,
		halfLife,
		halfLife,
		model.DefaultOOMBumpUpRatio,
		model.DefaultOOMMinBumpUp,
	)

	histogramOptions := config.CPUHistogramOptions
	if resourceName == model.ResourceMemory {
		histogramOptions = config.MemoryHistogramOptions
	}

	return &VPAEstimator{
		resourceName:           resourceName,
		spec:                   spec,
		histogramOptions:       histogramOptions,
		repeatedSampleInterval: repeatedSampleInterval,
		containers:             make(map[string]*vpaContainerHistory),
	}
}

// FeedSamples stores new usage samples for the given container.
//
// A batch identical to the last accepted one is a repeated delivery of the same
// metrics-server reading and is discarded, unless repeatedSampleInterval has
// elapsed, in which case it is a new reading that repeats the value.
func (e *VPAEstimator) FeedSamples(containerName string, samples []model.ResourceAmount) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.feedSamplesAt(containerName, samples, time.Now())
}

func (e *VPAEstimator) feedSamplesAt(containerName string, samples []model.ResourceAmount, now time.Time) {
	history := e.containers[containerName]
	if history == nil {
		history = &vpaContainerHistory{
			histogram: util.NewDecayingHistogram(e.histogramOptions, e.spec.HistogramDecayHalfLife.Duration),
		}
		e.containers[containerName] = history
	}

	if slices.Equal(samples, history.lastBatch) && now.Sub(history.lastAccepted) < e.repeatedSampleInterval {
		klog.V(5).InfoS(
			"VPA: repeated samples discarded",
			"resource", e.resourceName,
			"containerName", containerName,
			"received", len(samples),
		)
		return
	}
	history.lastBatch = slices.Clone(samples)
	history.lastAccepted = now

	inserted := 0
	for instance, sample := range samples {
		if sample < 0 {
			continue
		}

		if e.resourceName == model.ResourceMemory {
			e.addMemorySample(history, instance, sample, now)
		} else {
			history.histogram.AddSample(model.CoresFromCPUAmount(sample), vpaCPUSampleWeight, now)
		}

		if history.firstSampleStart.IsZero() || now.Before(history.firstSampleStart) {
			history.firstSampleStart = now
		}
		if now.After(history.lastSampleStart) {
			history.lastSampleStart = now
		}
		history.totalSamplesCount++
		inserted++
	}

	klog.V(4).InfoS(
		"VPA: samples inserted",
		"resource", e.resourceName,
		"containerName", containerName,
		"received", len(samples),
		"inserted", inserted,
		"totalSamplesCount", history.totalSamplesCount,
	)
}

// addMemorySample keeps one memory peak per aggregation interval for the given
// container instance, following ContainerState.addMemorySample: the peak is
// recorded at the end of its interval, and a higher usage within the interval
// replaces the previous peak in the histogram.
func (e *VPAEstimator) addMemorySample(history *vpaContainerHistory, instance int, usage model.ResourceAmount, ts time.Time) {
	for len(history.memoryWindows) <= instance {
		history.memoryWindows = append(history.memoryWindows, vpaMemoryWindow{})
	}
	window := &history.memoryWindows[instance]

	if window.windowEnd.IsZero() {
		window.windowEnd = ts
	}

	addNewPeak := false
	if ts.Before(window.windowEnd) {
		if window.peak != 0 && usage > window.peak {
			history.histogram.SubtractSample(model.BytesFromMemoryAmount(window.peak), vpaMemoryPeakWeight, window.windowEnd)
			addNewPeak = true
		}
	} else {
		interval := e.spec.MemoryAggregationInterval.Duration
		shift := ts.Sub(window.windowEnd).Truncate(interval) + interval
		window.windowEnd = window.windowEnd.Add(shift)
		window.peak = 0
		addNewPeak = true
	}

	if addNewPeak {
		history.histogram.AddSample(model.BytesFromMemoryAmount(usage), vpaMemoryPeakWeight, window.windowEnd)
		window.peak = usage
	}
}

// GetSingleResourceRecommendation returns a recommendation for the configured
// resource and the given container.
//
// Target, lower bound and upper bound are percentiles of the usage histogram
// plus the safety margin; the bounds are then scaled by the confidence
// multiplier. Constraints are applied last, like WithCPUMinResource and
// WithMemoryMinResource in the VPA, whose pod minimum matches constraints.Min.
//
// If no samples are available, the recommendation falls back to
// constraints.CurrentRequest. If CurrentRequest is not set, constraints.Min is
// used instead.
func (e *VPAEstimator) GetSingleResourceRecommendation(containerName string, constraints ContainerResourceConstraints) recommendation.SingleResourceRecommendation {
	e.mu.Lock()
	defer e.mu.Unlock()

	history := e.containers[containerName]
	if history == nil || history.totalSamplesCount == 0 {
		klog.V(4).InfoS("VPA: no samples",
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

	target := e.percentileWithMargin(history, e.spec.TargetPercentile)
	lowerBound := e.percentileWithMargin(history, e.spec.LowerBoundPercentile)
	upperBound := e.percentileWithMargin(history, e.spec.UpperBoundPercentile)

	confidence := history.confidence(e.spec.ConfidenceInterval.Duration)
	lowerBound = model.ScaleResource(lowerBound,
		math.Pow(1+vpaLowerBoundConfidenceMultiplier/confidence, vpaLowerBoundConfidenceExponent))
	upperBound = model.ScaleResource(upperBound,
		math.Pow(1+vpaUpperBoundConfidenceMultiplier/confidence, vpaUpperBoundConfidenceExponent))

	// The VPA applies the pod minimum inside the estimator chain, so its
	// uncapped target already includes it.
	uncappedTarget := model.ResourceAmountMax(target, constraints.Min)
	target = applyConstraints(target, constraints)
	lowerBound = applyConstraints(lowerBound, constraints)
	upperBound = applyConstraints(upperBound, constraints)

	klog.V(4).InfoS(
		"VPA: recommendation",
		"resource", e.resourceName,
		"containerName", containerName,
		"totalSamplesCount", history.totalSamplesCount,
		"confidence", confidence,
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

// percentileWithMargin returns the given percentile of the usage histogram
// plus the safety margin, as the VPA percentile and margin estimators do.
func (e *VPAEstimator) percentileWithMargin(history *vpaContainerHistory, percentile float64) model.ResourceAmount {
	value := history.histogram.Percentile(percentile)

	var base model.ResourceAmount
	if e.resourceName == model.ResourceMemory {
		base = model.MemoryAmountFromBytes(value)
	} else {
		base = model.CPUAmountFromCores(value)
	}

	return base + model.ScaleResource(base, e.spec.SafetyMarginFraction)
}

// confidence reproduces getConfidence from the VPA estimator: the available
// history measured in confidence intervals, limited by the number of samples
// at one sample per minute.
func (h *vpaContainerHistory) confidence(confidenceInterval time.Duration) float64 {
	lifespan := float64(h.lastSampleStart.Sub(h.firstSampleStart)) / float64(confidenceInterval)
	samplesAmount := float64(h.totalSamplesCount) / confidenceInterval.Minutes()

	return math.Min(lifespan, samplesAmount)
}
