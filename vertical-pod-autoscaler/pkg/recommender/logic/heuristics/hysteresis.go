package heuristics

import (
	"math"
	"sync"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/klog/v2"
)

const (
	CPUMinMillicores = 25.0
	CPUMinCores      = CPUMinMillicores / 1000.0
	MemoryMinMb      = 250.0
	MemoryMinBytes   = MemoryMinMb * 1024 * 1024
)

// =====================
// CPU Estimator
// =====================

type HysteresisCPUEstimator struct {
	mu         sync.Mutex
	prevOpt    map[string]float64
	minCores   float64
	percentile float64
	headroom   float64
}

func NewHysteresisCPUEstimator(percentile, headroom float64) *HysteresisCPUEstimator {
	return &HysteresisCPUEstimator{
		prevOpt:    make(map[string]float64),
		minCores:   CPUMinCores,
		percentile: percentile,
		headroom:   headroom,
	}
}

func (e *HysteresisCPUEstimator) GetCPUEstimation(s *model.AggregateContainerState, containerName string) model.ResourceAmount {
	if s.AggregateCPUUsage == nil || s.AggregateCPUUsage.IsEmpty() {
		klog.V(4).InfoS("Hysteresis CPU: sem amostras suficientes, retornando 0")
		return model.CPUAmountFromCores(0)
	}
	p := s.AggregateCPUUsage.Percentile(e.percentile)
	if math.IsNaN(p) || p <= 0 {
		klog.V(4).InfoS("Hysteresis CPU: percentil inválido, retornando 0")
		return model.CPUAmountFromCores(0)
	}
	key := containerName
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, hasPrev := e.prevOpt[key]
	if !hasPrev {
		result := math.Max(p, e.minCores)
		klog.V(4).InfoS("Hysteresis CPU: primeira estimativa", "p", p, "result", result)
		e.prevOpt[key] = result
		return model.CPUAmountFromCores(result)
	}
	lower := p * (1 - e.headroom)
	upper := p * (1 + e.headroom)
	var current float64
	if p >= prev {
		if prev >= lower {
			current = prev
			klog.V(4).InfoS("Hysteresis CPU: mantendo (p subiu, prev dentro da banda)", "prev", prev, "p", p, "lower", lower)
		} else {
			current = p
			klog.V(4).InfoS("Hysteresis CPU: atualizando para cima", "prev", prev, "p", p, "lower", lower)
		}
	} else {
		if prev <= upper {
			current = prev
			klog.V(4).InfoS("Hysteresis CPU: mantendo (p caiu, prev dentro da banda)", "prev", prev, "p", p, "upper", upper)
		} else {
			current = p
			klog.V(4).InfoS("Hysteresis CPU: atualizando para baixo", "prev", prev, "p", p, "upper", upper)
		}
	}
	current = math.Max(current, e.minCores)
	klog.V(4).InfoS("Hysteresis CPU resultado", "p", p, "prev", prev, "current", current)
	e.prevOpt[key] = current
	return model.CPUAmountFromCores(current)
}

// =====================
// Memory Estimator
// =====================

type HysteresisMemoryEstimator struct {
	mu         sync.Mutex
	prevOpt    map[string]float64
	minBytes   float64
	percentile float64
	headroom   float64
}

func NewHysteresisMemoryEstimator(percentile, headroom float64) *HysteresisMemoryEstimator {
	return &HysteresisMemoryEstimator{
		prevOpt:    make(map[string]float64),
		minBytes:   MemoryMinBytes,
		percentile: percentile,
		headroom:   headroom,
	}
}

func (e *HysteresisMemoryEstimator) GetMemoryEstimation(s *model.AggregateContainerState, containerName string) model.ResourceAmount {
	if s.AggregateMemoryPeaks == nil || s.AggregateMemoryPeaks.IsEmpty() {
		klog.V(4).InfoS("Hysteresis Memory: sem amostras suficientes, retornando 0")
		return model.MemoryAmountFromBytes(0)
	}
	p := s.AggregateMemoryPeaks.Percentile(e.percentile)
	if math.IsNaN(p) || p <= 0 {
		klog.V(4).InfoS("Hysteresis Memory: percentil inválido, retornando 0")
		return model.MemoryAmountFromBytes(0)
	}
	key := containerName
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, hasPrev := e.prevOpt[key]
	if !hasPrev {
		result := math.Max(p, e.minBytes)
		klog.V(4).InfoS("Hysteresis Memory: primeira estimativa", "p", p, "result", result)
		e.prevOpt[key] = result
		return model.MemoryAmountFromBytes(result)
	}
	lower := p * (1 - e.headroom)
	upper := p * (1 + e.headroom)
	var current float64
	if p >= prev {
		if prev >= lower {
			current = prev
			klog.V(4).InfoS("Hysteresis Memory: mantendo (p subiu, prev dentro da banda)", "prev", prev, "p", p, "lower", lower)
		} else {
			current = p
			klog.V(4).InfoS("Hysteresis Memory: atualizando para cima", "prev", prev, "p", p, "lower", lower)
		}
	} else {
		if prev <= upper {
			current = prev
			klog.V(4).InfoS("Hysteresis Memory: mantendo (p caiu, prev dentro da banda)", "prev", prev, "p", p, "upper", upper)
		} else {
			current = p
			klog.V(4).InfoS("Hysteresis Memory: atualizando para baixo", "prev", prev, "p", p, "upper", upper)
		}
	}
	current = math.Max(current, e.minBytes)
	klog.V(4).InfoS("Hysteresis Memory resultado", "p", p, "prev", prev, "current", current)
	e.prevOpt[key] = current
	return model.MemoryAmountFromBytes(current)
}
