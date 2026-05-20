package estimators

import (
	"math"
	"sort"
	"sync"
	"time"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/klog/v2"
)

type TimedSample struct {
	Value     model.ResourceAmount
	Timestamp time.Time
}

type PercentileHysteresisEstimator struct {
	mu            sync.Mutex
	resourceName  model.ResourceName
	samples       map[string][]TimedSample
	percentile    float64
	headroom      float64
	slidingWindow time.Duration
	percentileBuf []model.ResourceAmount
}

func NewPercentileHysteresisEstimator(
	resourceName model.ResourceName,
	percentile float64,
	headroom float64,
	slidingWindow time.Duration,
) *PercentileHysteresisEstimator {

	return &PercentileHysteresisEstimator{
		resourceName:  resourceName,
		samples:       make(map[string][]TimedSample),
		percentile:    percentile,
		headroom:      headroom,
		slidingWindow: slidingWindow,
		percentileBuf: make([]model.ResourceAmount, 0),
	}
}

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
	e.percentileBuf = buffer
	return result
}

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

func (e *PercentileHysteresisEstimator) GetResourceRecommendation(containerName string, constraints ResourceConstraints) recommendation.ResourceRecommendation {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.purgeSamples(containerName)

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

	return recommendation.ResourceRecommendation{
		Target:         target,
		LowerBound:     lowerBound,
		UpperBound:     upperBound,
		UncappedTarget: uncappedTarget,
	}
}

func scaleResourceAmount(amount model.ResourceAmount, factor float64) model.ResourceAmount {
	return model.ResourceAmount(math.Ceil(float64(amount) * factor))
}

func applyConstraints(amount model.ResourceAmount, constraints ResourceConstraints) model.ResourceAmount {
	if amount < constraints.Min {
		return constraints.Min
	}

	if amount > constraints.Max {
		return constraints.Max
	}

	return amount
}
