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

// AdrsMemV2 is the name used to select this heuristic in a VhapePolicy.
const AdrsMemV2 = "adrs-mem-v2"

// bytesPerMiB is used to express the algorithm's absolute floors and defaults,
// which are defined in MiB, in the byte-based unit used for memory resources.
const bytesPerMiB = 1024.0 * 1024.0

func init() {
	RegisterHeuristic(AdrsMemV2, func(config []byte) (HeuristicSpec, error) {
		spec := &AdrsMemV2Spec{}
		if err := json.Unmarshal(config, spec); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if spec.MinimumCoverageWindow.Duration >= spec.SlidingWindow.Duration {
			return nil, fmt.Errorf("invalid parameters: minimumCoverageWindow must be less than slidingWindow")
		}

		return spec, nil
	})
}

// AdrsMemV2Spec contains the configuration for the robust MAD + asymmetric
// EWMA heuristic (adrs-mem-v2).
//
// The heuristic is memory-only and self-tuning: apart from the windows used to
// retain and gate samples, its statistical behaviour is governed by internal
// constants rather than exposed parameters.
type AdrsMemV2Spec struct {
	SlidingWindow         metav1.Duration `json:"slidingWindow"`
	MinimumCoverageWindow metav1.Duration `json:"minimumCoverageWindow"`
}

// NewEstimator creates an adrs-mem-v2 estimator for the given resource.
func (s *AdrsMemV2Spec) NewEstimator(resourceName model.ResourceName) ResourceEstimator {
	return NewAdrsMemV2Estimator(
		resourceName,
		s.SlidingWindow.Duration,
		s.MinimumCoverageWindow.Duration,
	)
}

// AdrsMemV2Estimator estimates memory recommendations by combining a robust
// MAD-based tail estimate with a recent-persistent-peak defender, then applying
// asymmetric exponential smoothing (fast-up, slow-down) and hysteresis.
//
// Compared to the original robust-MAD + EWMA heuristic, v2 stops over-allocating
// and returns to the real usage level after a peak: the robust tail is capped at
// 1.15x the P99 of observed usage, the request floors use P99/P95 instead of the
// raw maximum, downward smoothing is faster (slow alpha 0.70), and downward
// hysteresis is looser (allows gradual ~1.5% drops).
//
// Samples are stored per container and purged according to a sliding window.
// A recommendation is produced only once the available samples cover the
// configured minimum span; until then the estimator falls back to the current
// request.
//
// This heuristic only supports memory. When configured for any other resource
// it degrades to returning the current request (or the minimum bound), leaving
// scaling decisions untouched.
type AdrsMemV2Estimator struct {
	mu                    sync.Mutex
	resourceName          model.ResourceName
	samples               map[string][]TimedSample
	slidingWindow         time.Duration
	minimumCoverageWindow time.Duration
}

func NewAdrsMemV2Estimator(
	resourceName model.ResourceName,
	slidingWindow time.Duration,
	minimumCoverageWindow time.Duration,
) *AdrsMemV2Estimator {

	return &AdrsMemV2Estimator{
		resourceName:          resourceName,
		samples:               make(map[string][]TimedSample),
		slidingWindow:         slidingWindow,
		minimumCoverageWindow: minimumCoverageWindow,
	}
}

// purgeSamples removes samples for the given container that are older than the
// estimator's sliding window. If all samples are expired, the container entry is
// removed from the samples map.
func (e *AdrsMemV2Estimator) purgeSamples(key string) {
	samples := e.samples[key]

	i := 0
	cutoff := time.Now().Add(-e.slidingWindow)
	for i < len(samples) && samples[i].Timestamp.Before(cutoff) {
		i++
	}

	if i == len(samples) {
		delete(e.samples, key)
		return
	}

	if i > 0 {
		newSlice := make([]TimedSample, len(samples)-i)
		copy(newSlice, samples[i:])
		e.samples[key] = newSlice
	}
}

// windowCoverage returns the duration between the oldest and newest samples
// stored for a container. Fewer than two samples have no coverage.
func (e *AdrsMemV2Estimator) windowCoverage(containerName string) time.Duration {
	samples := e.samples[containerName]
	if len(samples) < 2 {
		return 0
	}

	first := samples[0].Timestamp
	last := samples[len(samples)-1].Timestamp

	return last.Sub(first)
}

// FeedSamples stores new usage samples for the given container, then purges
// samples that fall outside the sliding window.
func (e *AdrsMemV2Estimator) FeedSamples(containerName string, samples []model.ResourceAmount) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	for _, sample := range samples {
		if sample < 0 {
			continue
		}

		e.samples[containerName] = append(e.samples[containerName], TimedSample{
			Value:     sample,
			Timestamp: now,
		})
	}

	e.purgeSamples(containerName)
}

// GetSingleResourceRecommendation returns a memory recommendation for the given
// container.
//
// When there is not enough window coverage, or the resource is not memory, the
// recommendation falls back to constraints.CurrentRequest (or constraints.Min
// when the current request is not set).
func (e *AdrsMemV2Estimator) GetSingleResourceRecommendation(containerName string, constraints ContainerResourceConstraints) recommendation.SingleResourceRecommendation {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.purgeSamples(containerName)

	fallback := constraints.CurrentRequest
	if fallback <= 0 {
		fallback = constraints.Min
	}

	// The heuristic is memory-only; for any other resource leave the request as
	// it is so that scaling is effectively deferred to another heuristic.
	if e.resourceName != model.ResourceMemory {
		return flatRecommendation(fallback)
	}

	coverage := e.windowCoverage(containerName)
	if coverage < e.minimumCoverageWindow {
		klog.V(4).InfoS("AdrsMemV2: not enough window coverage",
			"resource", e.resourceName,
			"containerName", containerName,
			"currentCoverage", coverage,
			"minimumCoverageWindow", e.minimumCoverageWindow,
			"slidingWindow", e.slidingWindow,
		)

		return flatRecommendation(fallback)
	}

	usage := make([]float64, 0, len(e.samples[containerName]))
	for _, sample := range e.samples[containerName] {
		usage = append(usage, float64(sample.Value))
	}

	prevRequest := float64(constraints.CurrentRequest)
	uncapped := recommendMemory(usage, prevRequest)

	uncappedTarget := model.ResourceAmount(math.Ceil(uncapped))
	target := applyConstraints(uncappedTarget, constraints)
	lowerBound := scaleResourceAmount(target, 0.9)
	upperBound := scaleResourceAmount(target, 1.1)

	klog.V(4).InfoS(
		"AdrsMemV2: recommendation",
		"resource", e.resourceName,
		"containerName", containerName,
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

// flatRecommendation returns a recommendation whose bounds all equal value.
func flatRecommendation(value model.ResourceAmount) recommendation.SingleResourceRecommendation {
	return recommendation.SingleResourceRecommendation{
		Target:         value,
		LowerBound:     value,
		UpperBound:     value,
		UncappedTarget: value,
	}
}

// recommendMemory computes a memory request (in the same byte unit as usage and
// prevRequest) using the robust-MAD + asymmetric-EWMA approach (v2).
//
// The usage slice is assumed to be free of NaNs and negative values. This is the
// direct translation of the reference heuristic; absolute floors and defaults
// originally expressed in MiB are scaled to bytes. The four v2 tunings are
// marked inline with "v2:".
func recommendMemory(usage []float64, prevRequest float64) float64 {
	if prevRequest < 0 {
		prevRequest = 0.0
	}

	if len(usage) == 0 {
		if prevRequest > 0 {
			return prevRequest
		}
		return 32.0 * bytesPerMiB
	}

	medianAll := median(usage)
	maxAll := maxFloat(usage)
	meanAll := mean(usage)
	// v2: robust upper level — a single transient spike barely moves P95, so
	// using it (instead of the raw max) stops one peak from pinning the request
	// near its top for the whole window.
	p95All := percentile(usage, 95)

	// Median Absolute Deviation (MAD) - robust scale.
	deviations := make([]float64, len(usage))
	for i, v := range usage {
		deviations[i] = math.Abs(v - medianAll)
	}
	mad := median(deviations)

	// std ≈ 1.4826 * MAD
	scaledStd := 1.4826 * mad

	// Robust coefficient of variation.
	robustCV := scaledStd / (medianAll + 1e-9)

	// Choose a conservative z-value depending on volatility. Common Gaussian
	// quantiles: p=0.999 -> 3.09, p=0.995 -> 2.576, p=0.99 -> 2.326,
	// p=0.98 -> 2.055.
	var z float64
	switch {
	case robustCV < 0.25:
		z = 3.09 // very stable -> can aim near 99.9%
	case robustCV < 0.5:
		z = 2.576 // stable -> ~99.5%
	case robustCV < 1.0:
		z = 2.326 // moderate -> ~99%
	default:
		z = 2.055 // very noisy -> ~98%
	}

	// Robust tail-based candidate: median + z * std_estimate.
	candidateRobust := medianAll + z*scaledStd
	// v2: level ceiling — on chronically volatile workloads the z*MAD term
	// inflates the target far above real peaks. Cap it at 1.15x the P99 of
	// observed usage so the request tracks the real tail instead of the
	// dispersion.
	p99All := percentile(usage, 99)
	candidateRobust = math.Min(candidateRobust, p99All*1.15)
	candidateRobust = math.Max(candidateRobust, medianAll*1.02) // at least slightly above median

	// Recent-peak defender: consider last min(1/8th, 30) samples.
	n := len(usage)
	recentN := int(math.Round(float64(n) / 8.0))
	if recentN < 1 {
		recentN = 1
	}
	if recentN > 30 {
		recentN = 30
	}
	recent := usage[n-recentN:]
	recentMax := maxFloat(recent)

	// A recent elevation is persistent only if multiple recent samples exceed
	// the robust median.
	elevatedRecentCount := 0
	for _, v := range recent {
		if v > candidateRobust*0.95 {
			elevatedRecentCount++
		}
	}
	persistentThreshold := int(0.25 * float64(recentN))
	if persistentThreshold < 2 {
		persistentThreshold = 2
	}
	persistentPeak := elevatedRecentCount >= persistentThreshold

	var candidateRecent float64
	if persistentPeak {
		candidateRecent = recentMax * 1.08 // modest headroom over recent max for safety
	} else {
		candidateRecent = recentMax * 1.02 // down-weight isolated peaks
	}

	// Blend: be conservative by taking the maximum of the robust tail and the
	// recent-defender.
	rawCandidate := math.Max(candidateRobust, candidateRecent)

	// Floor to avoid absurdly small values.
	minFloor := math.Max(12.0*bytesPerMiB, math.Max(0.4*medianAll, 0.2*meanAll))
	rawCandidate = math.Max(rawCandidate, minFloor)

	// If no existing request, adopt the candidate directly.
	if prevRequest <= 0.0 {
		return rawCandidate
	}

	// Asymmetric exponential smoothing: fast-up, slow-down.
	var smoothed float64
	if rawCandidate > prevRequest {
		fastAlpha := 0.35 // smaller alpha -> faster move toward candidate
		smoothed = prevRequest*fastAlpha + rawCandidate*(1.0-fastAlpha)
	} else {
		slowAlpha := 0.70 // v2: faster decay so it returns quickly after a peak
		smoothed = prevRequest*slowAlpha + rawCandidate*(1.0-slowAlpha)
	}

	// Hysteresis: require a meaningful absolute or relative change to update.
	relChange := smoothed / (prevRequest + 1e-9)
	absChange := math.Abs(smoothed - prevRequest)
	minAbsChange := math.Max(8.0*bytesPerMiB, 0.05*prevRequest)
	const minRelChangeUp = 1.08    // at least 8% up
	const minRelChangeDown = 0.985 // v2: allow gradual ~1.5% drops (was 0.96)

	shouldApply := false
	if smoothed > prevRequest {
		if relChange >= minRelChangeUp && absChange >= minAbsChange {
			shouldApply = true
		}
		if persistentPeak && recentMax >= prevRequest*1.15 {
			shouldApply = true
		}
	} else if smoothed < prevRequest {
		if relChange <= minRelChangeDown && absChange >= minAbsChange {
			shouldApply = true
		}
	}

	if !shouldApply {
		return prevRequest // keep previous request to maximize stability
	}

	newRequest := smoothed

	// Final clamps: never fall below a compact buffer of the robust upper level,
	// and avoid runaway upper bounds.
	// v2: P95 floor, not raw max.
	newRequest = math.Max(newRequest, math.Max(p95All*1.02, minFloor))
	absoluteMax := maxAll*5.0 + 512.0*bytesPerMiB
	newRequest = math.Min(newRequest, absoluteMax)

	return newRequest
}

// median returns the median of values, which must be non-empty.
func median(values []float64) float64 {
	buf := make([]float64, len(values))
	copy(buf, values)
	sort.Float64s(buf)

	mid := len(buf) / 2
	if len(buf)%2 == 1 {
		return buf[mid]
	}
	return (buf[mid-1] + buf[mid]) / 2.0
}

// mean returns the arithmetic mean of values, which must be non-empty.
func mean(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// maxFloat returns the largest value in values, which must be non-empty.
func maxFloat(values []float64) float64 {
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// percentile returns the p-th percentile (p in [0, 100]) of values using linear
// interpolation between closest ranks, matching numpy.percentile's default.
// values must be non-empty.
func percentile(values []float64, p float64) float64 {
	buf := make([]float64, len(values))
	copy(buf, values)
	sort.Float64s(buf)

	if len(buf) == 1 {
		return buf[0]
	}

	rank := (p / 100.0) * float64(len(buf)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return buf[lo]
	}

	frac := rank - float64(lo)
	return buf[lo] + frac*(buf[hi]-buf[lo])
}
