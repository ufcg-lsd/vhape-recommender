/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package logic

import (
	"math"
	"time"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/heuristics"
)

// ResourceEstimator is a function from AggregateContainerState to
// model.Resources, e.g. a prediction of resources needed by a group of
// containers.
type ResourceEstimator interface {
	GetResourceEstimation(s *model.AggregateContainerState) model.Resources
}

// CPUEstimator predicts CPU resources needed by a container
type CPUEstimator interface {
	GetCPUEstimation(s *model.AggregateContainerState, containerName string, currentUsagesCPU []float64) model.ResourceAmount
}

// MemoryEstimator predicts memory resources needed by a container
type MemoryEstimator interface {
	GetMemoryEstimation(s *model.AggregateContainerState, containerName string, currentUsagesMemory []float64) model.ResourceAmount
}

// combinedEstimator is a ResourceEstimator that combines two estimators: one for CPU and one for memory.
type combinedEstimator struct {
	cpuEstimator    CPUEstimator
	memoryEstimator MemoryEstimator
}

type percentileCPUEstimator struct {
	percentile float64
}

type percentileMemoryEstimator struct {
	percentile float64
}

// margins

type cpuMarginEstimator struct {
	marginFraction float64
	baseEstimator  CPUEstimator
}

type memoryMarginEstimator struct {
	marginFraction float64
	baseEstimator  MemoryEstimator
}

type cpuConfidenceMultiplier struct {
	multiplier         float64
	exponent           float64
	baseEstimator      CPUEstimator
	confidenceInterval time.Duration
}

type memoryConfidenceMultiplier struct {
	multiplier         float64
	exponent           float64
	baseEstimator      MemoryEstimator
	confidenceInterval time.Duration
}

type cpuMinResourceEstimator struct {
	minResource   model.ResourceAmount
	baseEstimator CPUEstimator
}

type memoryMinResourceEstimator struct {
	minResource   model.ResourceAmount
	baseEstimator MemoryEstimator
}

// NewCombinedEstimator returns a new combinedEstimator that uses provided estimators.
func NewCombinedEstimator(cpuEstimator CPUEstimator, memoryEstimator MemoryEstimator) ResourceEstimator {
	return &combinedEstimator{cpuEstimator, memoryEstimator}
}

// NewPercentileCPUEstimator returns a new percentileCPUEstimator that uses provided percentile.
func NewPercentileCPUEstimator(percentile float64) CPUEstimator {
	return &percentileCPUEstimator{percentile}
}

// NewPercentileMemoryEstimator returns a new percentileMemoryEstimator that uses provided percentile.
func NewPercentileMemoryEstimator(percentile float64) MemoryEstimator {
	return &percentileMemoryEstimator{percentile}
}

// NewMemoryEstimator returns a new percentileMemoryEstimator that uses provided percentile.
func NewMemoryEstimator(percentile float64) MemoryEstimator {
	return &percentileMemoryEstimator{percentile}
}

// GetCPUEstimation returns the CPU estimation for the given AggregateContainerState.
func (e *cpuMarginEstimator) GetCPUEstimation(s *model.AggregateContainerState, containerName string, _ []float64) model.ResourceAmount {
	base := e.baseEstimator.GetCPUEstimation(s, containerName, nil)
	margin := model.ScaleResource(base, e.marginFraction)
	return base + margin
}

// GetMemoryEstimation returns the memory estimation for the given AggregateContainerState.
func (e *memoryMarginEstimator) GetMemoryEstimation(s *model.AggregateContainerState, containerName string, _ []float64) model.ResourceAmount {
	base := e.baseEstimator.GetMemoryEstimation(s, containerName, nil)
	margin := model.ScaleResource(base, e.marginFraction)
	return base + margin
}

// WithCPUMargin returns a CPUEstimator that adds a margin to the base estimator.
func WithCPUMargin(marginFraction float64, baseEstimator CPUEstimator) CPUEstimator {
	return &cpuMarginEstimator{marginFraction: marginFraction, baseEstimator: baseEstimator}
}

// WithMemoryMargin returns a MemoryEstimator that adds a margin to the base estimator.
func WithMemoryMargin(marginFraction float64, baseEstimator MemoryEstimator) MemoryEstimator {
	return &memoryMarginEstimator{marginFraction: marginFraction, baseEstimator: baseEstimator}
}

// WithCPUConfidenceMultiplier return a CPUEstimator estimator
func WithCPUConfidenceMultiplier(multiplier, exponent float64, baseEstimator CPUEstimator, confidenceInterval time.Duration) CPUEstimator {
	return &cpuConfidenceMultiplier{
		multiplier:         multiplier,
		exponent:           exponent,
		baseEstimator:      baseEstimator,
		confidenceInterval: confidenceInterval,
	}
}

// WithMemoryConfidenceMultiplier returns a MemoryEstimator that scales the
func WithMemoryConfidenceMultiplier(multiplier, exponent float64, baseEstimator MemoryEstimator, confidenceInterval time.Duration) MemoryEstimator {
	return &memoryConfidenceMultiplier{
		multiplier:         multiplier,
		exponent:           exponent,
		baseEstimator:      baseEstimator,
		confidenceInterval: confidenceInterval,
	}
}

func (e *percentileCPUEstimator) GetCPUEstimation(s *model.AggregateContainerState, _ string, _ []float64) model.ResourceAmount {
	return model.CPUAmountFromCores(s.AggregateCPUUsage.Percentile(e.percentile))
}

func (e *percentileMemoryEstimator) GetMemoryEstimation(s *model.AggregateContainerState, _ string, _ []float64) model.ResourceAmount {
	return model.MemoryAmountFromBytes(s.AggregateMemoryPeaks.Percentile(e.percentile))
}

// Returns resources computed by the underlying estimators, scaled based on the
// confidence metric, which depends on the amount of available historical data.
// Each resource is transformed as follows:
//
//	scaledResource = originalResource * (1 + 1/confidence)^exponent.
//
// This can be used to widen or narrow the gap between the lower and upper bound
// estimators depending on how much input data is available to the estimators.
func (c *combinedEstimator) GetResourceEstimation(s *model.AggregateContainerState) model.Resources {
	return model.Resources{
		model.ResourceCPU:    c.cpuEstimator.GetCPUEstimation(s, "", nil),
		model.ResourceMemory: c.memoryEstimator.GetMemoryEstimation(s, "", nil),
	}
}

// Returns a non-negative real number that heuristically measures how much
// confidence the history aggregated in the AggregateContainerState provides.
// For a workload producing a steady stream of samples over N days at the rate
// of 1 sample per minute, this metric is equal to N.
// This implementation is a very simple heuristic which looks at the total count
// of samples and the time between the first and the last sample.
func getConfidence(s *model.AggregateContainerState, confidenceInterval time.Duration) float64 {
	// Distance between the first and the last observed sample time, measured in days.
	lifespanInDays := float64(s.LastSampleStart.Sub(s.FirstSampleStart)) / float64(confidenceInterval)
	// Total count of samples normalized such that it equals the number of days for
	// frequency of 1 sample/minute.
	samplesAmount := float64(s.TotalSamplesCount) / confidenceInterval.Minutes()
	return math.Min(lifespanInDays, samplesAmount)
}

func (e *cpuConfidenceMultiplier) GetCPUEstimation(s *model.AggregateContainerState, containerName string, _ []float64) model.ResourceAmount {
	confidence := getConfidence(s, e.confidenceInterval)
	base := e.baseEstimator.GetCPUEstimation(s, containerName, nil)
	return model.ScaleResource(base, math.Pow(1.+e.multiplier/confidence, e.exponent))
}

func (e *memoryConfidenceMultiplier) GetMemoryEstimation(s *model.AggregateContainerState, containerName string, _ []float64) model.ResourceAmount {
	confidence := getConfidence(s, e.confidenceInterval)
	base := e.baseEstimator.GetMemoryEstimation(s, containerName, nil)
	return model.ScaleResource(base, math.Pow(1.+e.multiplier/confidence, e.exponent))
}

// WithCPUMinResource returns a CPUEstimator that returns at least minResource
func WithCPUMinResource(minResource model.ResourceAmount, baseEstimator CPUEstimator) CPUEstimator {
	return &cpuMinResourceEstimator{minResource, baseEstimator}
}

// WithMemoryMinResource returns a MemoryEstimator that returns at least minResource
func WithMemoryMinResource(minResource model.ResourceAmount, baseEstimator MemoryEstimator) MemoryEstimator {
	return &memoryMinResourceEstimator{minResource, baseEstimator}
}

func (e *cpuMinResourceEstimator) GetCPUEstimation(s *model.AggregateContainerState, containerName string, _ []float64) model.ResourceAmount {
	return model.ResourceAmountMax(e.baseEstimator.GetCPUEstimation(s, containerName, nil), e.minResource)
}

func (e *memoryMinResourceEstimator) GetMemoryEstimation(s *model.AggregateContainerState, containerName string, _ []float64) model.ResourceAmount {
	return model.ResourceAmountMax(e.baseEstimator.GetMemoryEstimation(s, containerName, nil), e.minResource)
}

// NewConstMemoryEstimator returns a Memory estimator that always returns the same value
func NewConstMemoryEstimator(memory model.ResourceAmount) MemoryEstimator {
	return &constMemoryEstimator{memory}
}

type constCPUEstimator struct {
	value model.ResourceAmount
}

type constMemoryEstimator struct {
	value model.ResourceAmount
}

func (e *constCPUEstimator) GetCPUEstimation(_ *model.AggregateContainerState, _ string, _ []float64) model.ResourceAmount {
	return e.value
}

func (e *constMemoryEstimator) GetMemoryEstimation(_ *model.AggregateContainerState, _ string, _ []float64) model.ResourceAmount {
	return e.value
}

// NewConstCPUEstimator returns a CPU estimator that always returns the same value
func NewConstCPUEstimator(cpu model.ResourceAmount) CPUEstimator {
	return &constCPUEstimator{cpu}
}

// heuristic names
const (
    HeuristicPercentileHysteresis = "percentile-hysteresis"
)

// cachedEstimators holds the estimators for a given policy — persists between cycles.
type cachedEstimators struct {
    baseCPU          *heuristics.HysteresisCPUEstimator
    baseMemory       *heuristics.HysteresisMemoryEstimator
    targetCPU        CPUEstimator
    targetMemory     MemoryEstimator
    lowerBoundCPU    CPUEstimator
    lowerBoundMemory MemoryEstimator
    upperBoundCPU    CPUEstimator
    upperBoundMemory MemoryEstimator
}

// selectHeuristic returns the estimators based on the VhapePolicy.
func selectHeuristic(policy *VhapePolicy) *cachedEstimators {
    switch policy.Spec.Heuristic {
    default:
        baseCPU := heuristics.NewHysteresisCPUEstimator(policy.Spec.CPU.Percentile)
        baseMem := heuristics.NewHysteresisMemoryEstimator(policy.Spec.Memory.Percentile)
        cpuH := policy.Spec.CPU.Headroom
        memH := policy.Spec.Memory.Headroom
        return &cachedEstimators{
            baseCPU:          baseCPU,
            baseMemory:       baseMem,
            targetCPU:        WithCPUMargin(cpuH, baseCPU),
            targetMemory:     WithMemoryMargin(memH, baseMem),
            lowerBoundCPU:    WithCPUMargin(cpuH-policy.Spec.CPU.LowerBound*(1+cpuH), baseCPU),
            lowerBoundMemory: WithMemoryMargin(memH-policy.Spec.Memory.LowerBound*(1+memH), baseMem),
            upperBoundCPU:    WithCPUMargin(cpuH+policy.Spec.CPU.UpperBound*(1+cpuH), baseCPU),
            upperBoundMemory: WithMemoryMargin(memH+policy.Spec.Memory.UpperBound*(1+memH), baseMem),
        }
    }
}