package estimators

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/klog/v2"
)

const (
	AdaptivePercentile = "adaptive-percentile"
	vhapeMin           = 1e-3
)

func init() {
	RegisterHeuristic(AdaptivePercentile, func(config []byte) (HeuristicSpec, error) {
		spec := &AdaptivePercentileSpec{}
		if err := json.Unmarshal(config, spec); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		return spec, nil
	})
}

// AdaptivePercentileSpec contains configuration for the adaptive-percentile heuristic.
// This heuristic adapts the percentile based on workload variability (coefficient of variation).
type AdaptivePercentileSpec struct {
	// SlidingWindow defines the time window for tracking samples (e.g., "24h")
	SlidingWindow metav1.Duration `json:"slidingWindow"`
}

// NewEstimator creates an adaptive-percentile estimator for the given resource.
func (s *AdaptivePercentileSpec) NewEstimator(resourceName model.ResourceName) ResourceEstimator {
	return NewAdaptivePercentileEstimator(resourceName, s.SlidingWindow.Duration)
}

// AdaptivePercentileEstimator implements adaptive percentile-based recommendations.
// It adjusts the percentile based on workload variability (CV):
// - Stable workloads (low CV) use lower percentiles (more aggressive)
// - Volatile workloads (high CV) use higher percentiles (more conservative)
//
// Additional features:
// - Protects against rare extreme spikes
// - Reacts faster to recent growth
// - Dynamic ramp limits
// - Hysteresis (~10%) to avoid noisy micro-adjustments
// - Asymmetric smoothing (fast up, slow down)
type AdaptivePercentileEstimator struct {
	mu            sync.Mutex
	resourceName  model.ResourceName
	samples       map[string][]TimedSample
	slidingWindow time.Duration
	buffer        []float64
}

// NewAdaptivePercentileEstimator creates a new adaptive percentile estimator.
func NewAdaptivePercentileEstimator(
	resourceName model.ResourceName,
	slidingWindow time.Duration,
) *AdaptivePercentileEstimator {
	return &AdaptivePercentileEstimator{
		resourceName:  resourceName,
		samples:       make(map[string][]TimedSample),
		slidingWindow: slidingWindow,
		buffer:        make([]float64, 0),
	}
}

// FeedSamples stores new usage samples for the given container.
func (e *AdaptivePercentileEstimator) FeedSamples(containerName string, samples []model.ResourceAmount) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
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

	e.purgeSamples(containerName)

	klog.V(4).InfoS(
		"AdaptivePercentile: samples inserted",
		"resource", e.resourceName,
		"containerName", containerName,
		"received", len(samples),
		"inserted", inserted,
		"totalCurrentSamples", len(e.samples[containerName]),
	)
}

// purgeSamples removes samples older than the sliding window.
func (e *AdaptivePercentileEstimator) purgeSamples(containerName string) {
	samples := e.samples[containerName]
	if len(samples) == 0 {
		return
	}

	cutoff := time.Now().Add(-e.slidingWindow)
	i := 0
	for i < len(samples) && samples[i].Timestamp.Before(cutoff) {
		i++
	}

	if i == len(samples) {
		delete(e.samples, containerName)
		return
	}

	if i > 0 {
		newSlice := make([]TimedSample, len(samples)-i)
		copy(newSlice, samples[i:])
		e.samples[containerName] = newSlice
	}
}

// calculatePercentile returns the value at the given percentile.
func (e *AdaptivePercentileEstimator) calculatePercentile(vals []float64, percentile float64) float64 {
	if len(vals) == 0 {
		return 0
	}

	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	idx := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// toFloat64Slice converts ResourceAmount samples to float64, filtering out invalid values.
func (e *AdaptivePercentileEstimator) toFloat64Slice(samples []TimedSample) []float64 {
	buffer := e.buffer[:0]
	for _, s := range samples {
		val := float64(s.Value)
		// Check if value is finite (not NaN or Inf)
		if !math.IsNaN(val) && !math.IsInf(val, 0) {
			buffer = append(buffer, math.Max(val, 0))
		}
	}
	e.buffer = buffer
	return buffer
}

// GetSingleResourceRecommendation returns an adaptive-percentile recommendation.
func (e *AdaptivePercentileEstimator) GetSingleResourceRecommendation(
	containerName string,
	constraints ContainerResourceConstraints,
) recommendation.SingleResourceRecommendation {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.purgeSamples(containerName)

	samples := e.samples[containerName]
	if len(samples) == 0 {
		fallback := constraints.CurrentRequest
		if fallback <= 0 {
			fallback = constraints.Min
		}

		klog.V(4).InfoS(
			"AdaptivePercentile: no samples, using fallback",
			"resource", e.resourceName,
			"containerName", containerName,
			"fallback", fallback,
		)

		return recommendation.SingleResourceRecommendation{
			Target:         fallback,
			LowerBound:     fallback,
			UpperBound:     fallback,
			UncappedTarget: fallback,
		}
	}

	// Convert to float64 for calculations
	vals := e.toFloat64Slice(samples)
	n := len(vals)
	if n == 0 {
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

	// Calculate percentiles and statistics
	p50 := e.calculatePercentile(vals, 0.50)
	p55 := e.calculatePercentile(vals, 0.55)
	p60 := e.calculatePercentile(vals, 0.60)
	p75 := e.calculatePercentile(vals, 0.75)
	p85 := e.calculatePercentile(vals, 0.85)
	p90 := e.calculatePercentile(vals, 0.90)
	p95 := e.calculatePercentile(vals, 0.95)

	vmax := vals[0]
	mean := 0.0
	variance := 0.0

	for _, v := range vals {
		if v > vmax {
			vmax = v
		}
		mean += v
	}
	mean /= float64(n)

	for _, v := range vals {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(n)
	std := math.Sqrt(variance)

	cv := 0.0
	if mean > 0 {
		cv = std / mean
	}

	// Detect extreme spike: very high peak but small fraction above P95
	fracAboveP95 := 0.0
	if p95 > 0 {
		countAboveP95 := 0
		for _, v := range vals {
			if v > p95 {
				countAboveP95++
			}
		}
		fracAboveP95 = float64(countAboveP95) / float64(n)
	}

	extremeSpike := (vmax > 4.0*p95) && (fracAboveP95 < 0.01)

	// Recent growth: median of last 20% of samples vs P50
	k := int(math.Max(3, 0.2*float64(n)))
	if k > n {
		k = n
	}
	recent := vals[n-k:]
	recentMedian := e.calculatePercentile(recent, 0.50)
	recentGrowth := recentMedian > p50*1.18

	// Select base percentile and headroom based on CV
	var baseVal, headroom float64
	switch {
	case cv < 0.20:
		baseVal, headroom = p55, 0.06
	case cv < 0.35:
		baseVal, headroom = p60, 0.07
	case cv < 0.80:
		baseVal, headroom = p75, 0.10
	case cv < 1.20:
		baseVal, headroom = p85, 0.14
	default:
		baseVal, headroom = p90, 0.18
	}

	// Extreme spike protection
	immediateIncreaseCap := 1.8
	if extremeSpike {
		baseVal = math.Min(baseVal, p75)
		headroom = math.Min(headroom, 0.10)
		immediateIncreaseCap = 1.4
	} else if recentGrowth {
		immediateIncreaseCap = 2.0
	}

	target := baseVal * (1.0 + headroom)
	prevRequest := float64(constraints.CurrentRequest)

	// Hysteresis: avoid noisy micro-adjustments
	changeTol := 0.10
	if target*(1.0-changeTol) <= prevRequest && prevRequest <= target*(1.0+changeTol) {
		newRequest := prevRequest
		recommendation := e.buildRecommendation(model.ResourceAmount(newRequest), constraints)

		klog.V(4).InfoS(
			"AdaptivePercentile: hysteresis hold",
			"resource", e.resourceName,
			"containerName", containerName,
			"cv", fmt.Sprintf("%.3f", cv),
			"target", fmt.Sprintf("%.0f", target),
			"recommendation", recommendation.Target,
		)

		return recommendation
	}

	// Dynamic ramp limits: shrink more on stable workloads
	var maxDecreaseFactor float64
	switch {
	case cv < 0.35:
		maxDecreaseFactor = 0.50
	case cv < 0.80:
		maxDecreaseFactor = 0.60
	default:
		maxDecreaseFactor = 0.70
	}

	var allowed float64
	if target < prevRequest {
		allowed = math.Max(target, prevRequest*maxDecreaseFactor)
	} else {
		allowed = math.Min(target, prevRequest*immediateIncreaseCap)
	}

	// Median-relative floor
	allowed = math.Max(allowed, math.Max(vhapeMin, p50*0.25))

	// Asymmetric smoothing: fast up, slow down
	var alpha float64
	if allowed >= prevRequest {
		if recentGrowth {
			alpha = 0.92
		} else {
			alpha = 0.80
		}
	} else {
		alpha = 0.45
	}

	newRequest := prevRequest*(1.0-alpha) + allowed*alpha

	// Minimum-change guard to avoid churn
	minChangeRatio := 0.06
	if math.Abs(newRequest-prevRequest)/math.Max(prevRequest, vhapeMin) < minChangeRatio {
		newRequest = prevRequest
	}

	recommendation := e.buildRecommendation(model.ResourceAmount(newRequest), constraints)

	klog.V(4).InfoS(
		"AdaptivePercentile: recommendation",
		"resource", e.resourceName,
		"containerName", containerName,
		"cv", fmt.Sprintf("%.3f", cv),
		"baseVal", fmt.Sprintf("%.0f", baseVal),
		"target", fmt.Sprintf("%.0f", target),
		"allowed", fmt.Sprintf("%.0f", allowed),
		"recommendation", recommendation.Target,
		"lowerBound", recommendation.LowerBound,
		"upperBound", recommendation.UpperBound,
	)

	return recommendation
}

// buildRecommendation creates the final recommendation with bounds.
func (e *AdaptivePercentileEstimator) buildRecommendation(
	target model.ResourceAmount,
	constraints ContainerResourceConstraints,
) recommendation.SingleResourceRecommendation {
	// Apply constraints
	target = applyConstraints(target, constraints)

	// Set bounds around the target (±10%)
	lowerBound := scaleResourceAmount(target, 0.9)
	upperBound := scaleResourceAmount(target, 1.1)

	return recommendation.SingleResourceRecommendation{
		Target:         target,
		LowerBound:     lowerBound,
		UpperBound:     upperBound,
		UncappedTarget: target,
	}
}
