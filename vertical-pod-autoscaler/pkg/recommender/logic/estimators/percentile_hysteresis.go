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

// PercentileHysteresis is the name used to select this heuristic in a VhapePolicy.
const PercentileHysteresis = "percentile-hysteresis"

func init() {
	RegisterHeuristic(PercentileHysteresis, func(config []byte) (HeuristicSpec, error) {
		spec := &PercentileHysteresisSpec{}
		if err := json.Unmarshal(config, spec); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if spec.MinimumCoverageWindow.Duration >= spec.SlidingWindow.Duration {
			return nil, fmt.Errorf("invalid parameters: minimumCoverageWindow must be less than slidingWindow")
		}

		return spec, nil
	})
}

// PercentileHysteresisSpec contains the configuration for the percentile
// hysteresis heuristic.
type PercentileHysteresisSpec struct {
	Percentile            float64         `json:"percentile"`
	Headroom              float64         `json:"headroom"`
	SlidingWindow         metav1.Duration `json:"slidingWindow"`
	MinimumCoverageWindow metav1.Duration `json:"minimumCoverageWindow"`
}

// NewEstimator creates a percentile hysteresis estimator for the given resource.
func (s *PercentileHysteresisSpec) NewEstimator(resourceName model.ResourceName) ResourceEstimator {
	return NewPercentileHysteresisEstimator(
		resourceName,
		s.Percentile,
		s.Headroom,
		s.SlidingWindow.Duration,
		s.MinimumCoverageWindow.Duration,
	)
}

// TimedSample represents a resource usage sample associated with the time at
// which it was collected.
//
// The timestamp is used to keep only samples that fall within the estimator's
// sliding window.
type TimedSample struct {
	Value     model.ResourceAmount
	Timestamp time.Time
}

// PercentileHysteresisEstimator estimates resource recommendations using a
// percentile of recent usage samples plus a configurable headroom.
//
// Samples are stored per container and are periodically purged according to a
// sliding window expiration. A recommendation is produced only when the available
// samples cover the configured minimum time span. Until enough coverage is available,
// the estimator falls back to the current request, or to the minimum allowed
// value when the current request is not set.
type PercentileHysteresisEstimator struct {
	mu                    sync.Mutex
	resourceName          model.ResourceName
	samples               map[string][]TimedSample
	percentile            float64
	headroom              float64
	slidingWindow         time.Duration
	percentileBuf         []model.ResourceAmount
	minimumCoverageWindow time.Duration
}

func NewPercentileHysteresisEstimator(
	resourceName model.ResourceName,
	percentile float64,
	headroom float64,
	slidingWindow time.Duration,
	minimumCoverageWindow time.Duration,
) *PercentileHysteresisEstimator {

	return &PercentileHysteresisEstimator{
		resourceName:          resourceName,
		samples:               make(map[string][]TimedSample),
		percentile:            percentile,
		headroom:              headroom,
		slidingWindow:         slidingWindow,
		percentileBuf:         make([]model.ResourceAmount, 0),
		minimumCoverageWindow: minimumCoverageWindow,
	}
}

// purgeSamples removes samples for the given container that are older than the
// estimator's sliding window.
//
// If all samples are expired, the container entry is removed from the samples
// map.
func (e *PercentileHysteresisEstimator) purgeSamples(key string) {
	samples := e.samples[key]
	before := len(samples)

	i := 0
	cutoff := time.Now().Add(-e.slidingWindow)
	for i < len(samples) && samples[i].Timestamp.Before(cutoff) {
		i++
	}

	if i == len(samples) {
		delete(e.samples, key)

		klog.V(4).InfoS(
			"Hysteresis: purge",
			"resource", e.resourceName,
			"containerName", key,
			"discarded", len(samples),
			"remaining", 0,
		)
		return
	}

	if i > 0 {
		newSlice := make([]TimedSample, len(samples)-i)
		copy(newSlice, samples[i:])
		e.samples[key] = newSlice
	}

	after := len(e.samples[key])

	klog.V(4).InfoS(
		"Hysteresis: purge",
		"resource", e.resourceName,
		"containerName", key,
		"discarded", before-after,
		"remaining", after,
	)
}

// calculatePercentile returns the configured percentile for the samples stored
// for the given container.
//
// The method assumes samples have already been purged.
func (e *PercentileHysteresisEstimator) calculatePercentile(key string) model.ResourceAmount {
	samples := e.samples[key]

	if len(samples) == 0 {
		return 0
	}

	buffer := e.percentileBuf[:0]
	for _, sample := range samples {
		buffer = append(buffer, sample.Value)
	}

	sort.Slice(buffer, func(i, j int) bool {
		return buffer[i] < buffer[j]
	})

	idx := int(math.Ceil(e.percentile*float64(len(buffer)))) - 1
	if idx < 0 {
		idx = 0
	}
	result := buffer[idx]
	e.percentileBuf = buffer[:0]
	return result
}

// FeedSamples stores new usage samples for the given container.
// After insertion, expired samples are purged according to the configured sliding window.
func (e *PercentileHysteresisEstimator) FeedSamples(containerName string, samples []model.ResourceAmount) {
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

	klog.V(4).InfoS(
		"Hysteresis: samples inserted",
		"resource", e.resourceName,
		"containerName", containerName,
		"received", len(samples),
		"inserted", inserted,
		"totalCurrentSamples", len(e.samples[containerName]),
	)

	e.purgeSamples(containerName)
}

// GetSingleResourceRecommendation returns a recommendation for the configured
// resource and the given container.
//
// The recommendation is calculated by taking the configured percentile of the
// container's recent usage samples, adding headroom, and applying the provided
// constraints.
//
// If the available samples do not cover enough of the sliding window, the
// recommendation falls back to constraints.CurrentRequest. If CurrentRequest is
// not set, constraints.Min is used instead.
func (e *PercentileHysteresisEstimator) GetSingleResourceRecommendation(containerName string, constraints ContainerResourceConstraints) recommendation.SingleResourceRecommendation {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.purgeSamples(containerName)

	if !e.hasEnoughWindowCoverage(containerName) {
		klog.V(4).InfoS("PercentileHysteresis: not enough window coverage",
			"resource", e.resourceName,
			"containerName", containerName,
			"minimumCoverageWindow", e.minimumCoverageWindow,
			"slidingWindow", e.slidingWindow,
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

	base := e.calculatePercentile(containerName)

	uncappedTarget := scaleResourceAmount(base, 1+e.headroom)
	target := applyConstraints(uncappedTarget, constraints)

	lowerBound := scaleResourceAmount(target, 0.9)
	upperBound := scaleResourceAmount(target, 1.1)

	klog.V(4).InfoS(
		"Hysteresis: recommendation",
		"resource", e.resourceName,
		"containerName", containerName,
		"base", base,
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

// hasEnoughWindowCoverage reports whether the stored samples for a container
// cover enough of the sliding window to produce a recommendation.
func (e *PercentileHysteresisEstimator) hasEnoughWindowCoverage(containerName string) bool {
	samples := e.samples[containerName]
	if len(samples) < 2 {
		return false
	}

	first := samples[0].Timestamp
	last := samples[len(samples)-1].Timestamp

	coverage := last.Sub(first)

	return coverage >= e.minimumCoverageWindow
}

// scaleResourceAmount multiplies a resource amount by the given factor and
// rounds the result up, keeping resources in int64 type.
func scaleResourceAmount(amount model.ResourceAmount, factor float64) model.ResourceAmount {
	return model.ResourceAmount(math.Ceil(float64(amount) * factor))
}

// applyConstraints clamps a resource amount to the provided minimum and maximum
// bounds.
// A maximum value less than or equal to zero is treated as unbounded.
func applyConstraints(amount model.ResourceAmount, constraints ContainerResourceConstraints) model.ResourceAmount {
	if amount < constraints.Min {
		return constraints.Min
	}

	if constraints.Max > 0 && amount > constraints.Max {
		return constraints.Max
	}

	return amount
}
