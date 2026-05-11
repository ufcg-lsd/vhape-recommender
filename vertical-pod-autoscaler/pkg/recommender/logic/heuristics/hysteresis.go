package heuristics

import (
    "math"
    "sort"
    "sync"
    "time"

    "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
    "k8s.io/klog/v2"
)

const (
    CPUMinMillicores = 25.0
    CPUMinCores      = CPUMinMillicores / 1000.0
    MemoryMinMb      = 250.0
    MemoryMinBytes   = MemoryMinMb * 1024 * 1024
    SlidingWindow    = 24 * time.Hour
)

// percentileBufPool reusa o slice temporário de calculatePercentile para evitar
// alocações de ~115KB por container por ciclo com 14400 amostras.
var percentileBufPool = sync.Pool{
    New: func() interface{} {
        buf := make([]float64, 0, 1500)
        return &buf
    },
}

type TimedSample struct {
    Value     float64
    Timestamp time.Time
}

// =====================
// CPU Estimator
// =====================

type HysteresisCPUEstimator struct {
    mu         sync.Mutex
    prevOpt    map[string]float64
    samples    map[string][]TimedSample
    minCores   float64
    percentile float64
    headroom   float64
    gcCounter  int
}

func NewHysteresisCPUEstimator(percentile, headroom float64) *HysteresisCPUEstimator {
    return &HysteresisCPUEstimator{
        prevOpt:    make(map[string]float64),
        samples:    make(map[string][]TimedSample),
        minCores:   CPUMinCores,
        percentile: percentile,
        headroom:   headroom,
    }
}

func (e *HysteresisCPUEstimator) purgeSamples(key string) {
    cutoff := time.Now().Add(-SlidingWindow)
    samples := e.samples[key]
    i := 0
    for i < len(samples) && samples[i].Timestamp.Before(cutoff) {
        i++
    }
    if i > 0 {
        newSlice := make([]TimedSample, len(samples)-i)
        copy(newSlice, samples[i:])
        e.samples[key] = newSlice
    }
}

func (e *HysteresisCPUEstimator) gcOrphanedKeys() {
    cutoff := time.Now().Add(-SlidingWindow)
    deleted := 0
    for key, samples := range e.samples {
        if len(samples) == 0 {
            delete(e.samples, key)
            delete(e.prevOpt, key)
            deleted++
            continue
        }
        if samples[len(samples)-1].Timestamp.Before(cutoff) {
            delete(e.samples, key)
            delete(e.prevOpt, key)
            deleted++
        }
    }
    klog.V(4).InfoS("Hysteresis CPU: GC de chaves órfãs", "chavesDeletadas", deleted)
}

func (e *HysteresisCPUEstimator) calculatePercentile(key string) float64 {
    samples := e.samples[key]
    if len(samples) == 0 {
        return 0
    }
    bufPtr := percentileBufPool.Get().(*[]float64)
    values := (*bufPtr)[:0]
    if cap(values) < len(samples) {
        values = make([]float64, len(samples))
    } else {
        values = values[:len(samples)]
    }
    for i, s := range samples {
        values[i] = s.Value
    }
    sort.Float64s(values)
    idx := int(math.Ceil(e.percentile*float64(len(values)))) - 1
    if idx < 0 {
        idx = 0
    }
    result := values[idx]
    *bufPtr = values
    percentileBufPool.Put(bufPtr)
    return result
}

func (e *HysteresisCPUEstimator) GetCPUEstimation(s *model.AggregateContainerState, containerName string, currentUsagesCPU []float64) model.ResourceAmount {
    key := containerName
    e.mu.Lock()
    defer e.mu.Unlock()

    now := time.Now()
    for _, usage := range currentUsagesCPU {
        if usage > 0 {
            e.samples[key] = append(e.samples[key], TimedSample{
                Value:     usage / 1000.0,
                Timestamp: now,
            })
            klog.V(5).InfoS("Hysteresis CPU: amostra inserida", "containerName", key, "valueCores", usage/1000.0)
        }
    }
    klog.V(4).InfoS("Hysteresis CPU: amostras inseridas", "containerName", key, "podsNoCiclo", len(currentUsagesCPU), "totalAmostras", len(e.samples[key]))

    before := len(e.samples[key])
    e.purgeSamples(key)
    after := len(e.samples[key])
    klog.V(4).InfoS("Hysteresis CPU: purge", "containerName", key, "descartadas", before-after, "restantes", after)
    e.gcCounter++
    if e.gcCounter >= 60 {
        e.gcOrphanedKeys()
        e.gcCounter = 0
    }

    p := e.calculatePercentile(key)
    if p <= 0 {
        klog.V(4).InfoS("Hysteresis CPU: sem amostras suficientes, retornando mínimo")
        return model.CPUAmountFromCores(e.minCores)
    }

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
    samples    map[string][]TimedSample
    minBytes   float64
    percentile float64
    headroom   float64
    gcCounter  int
}

func NewHysteresisMemoryEstimator(percentile, headroom float64) *HysteresisMemoryEstimator {
    return &HysteresisMemoryEstimator{
        prevOpt:    make(map[string]float64),
        samples:    make(map[string][]TimedSample),
        minBytes:   MemoryMinBytes,
        percentile: percentile,
        headroom:   headroom,
    }
}

func (e *HysteresisMemoryEstimator) purgeSamples(key string) {
    cutoff := time.Now().Add(-SlidingWindow)
    samples := e.samples[key]
    i := 0
    for i < len(samples) && samples[i].Timestamp.Before(cutoff) {
        i++
    }
    if i > 0 {
        newSlice := make([]TimedSample, len(samples)-i)
        copy(newSlice, samples[i:])
        e.samples[key] = newSlice
    }
}

func (e *HysteresisMemoryEstimator) gcOrphanedKeys() {
    cutoff := time.Now().Add(-SlidingWindow)
    deleted := 0
    for key, samples := range e.samples {
        if len(samples) == 0 {
            delete(e.samples, key)
            delete(e.prevOpt, key)
            deleted++
            continue
        }
        if samples[len(samples)-1].Timestamp.Before(cutoff) {
            delete(e.samples, key)
            delete(e.prevOpt, key)
            deleted++
        }
    }
    klog.V(4).InfoS("Hysteresis Memory: GC de chaves órfãs", "chavesDeletadas", deleted)
}

func (e *HysteresisMemoryEstimator) calculatePercentile(key string) float64 {
    samples := e.samples[key]
    if len(samples) == 0 {
        return 0
    }
    bufPtr := percentileBufPool.Get().(*[]float64)
    values := (*bufPtr)[:0]
    if cap(values) < len(samples) {
        values = make([]float64, len(samples))
    } else {
        values = values[:len(samples)]
    }
    for i, s := range samples {
        values[i] = s.Value
    }
    sort.Float64s(values)
    idx := int(math.Ceil(e.percentile*float64(len(values)))) - 1
    if idx < 0 {
        idx = 0
    }
    result := values[idx]
    *bufPtr = values
    percentileBufPool.Put(bufPtr)
    return result
}

func (e *HysteresisMemoryEstimator) GetMemoryEstimation(s *model.AggregateContainerState, containerName string, currentUsagesMemory []float64) model.ResourceAmount {
    key := containerName
    e.mu.Lock()
    defer e.mu.Unlock()

    now := time.Now()
    for _, usage := range currentUsagesMemory {
        if usage > 0 {
            e.samples[key] = append(e.samples[key], TimedSample{
                Value:     usage,
                Timestamp: now,
            })
            klog.V(5).InfoS("Hysteresis Memory: amostra inserida", "containerName", key, "valueBytes", usage)
        }
    }
    klog.V(4).InfoS("Hysteresis Memory: amostras inseridas", "containerName", key, "podsNoCiclo", len(currentUsagesMemory), "totalAmostras", len(e.samples[key]))

    before := len(e.samples[key])
    e.purgeSamples(key)
    after := len(e.samples[key])
    klog.V(4).InfoS("Hysteresis Memory: purge", "containerName", key, "descartadas", before-after, "restantes", after)
    e.gcCounter++
    if e.gcCounter >= 60 {
        e.gcOrphanedKeys()
        e.gcCounter = 0
    }

    p := e.calculatePercentile(key)
    if p <= 0 {
        klog.V(4).InfoS("Hysteresis Memory: sem amostras suficientes, retornando mínimo")
        return model.MemoryAmountFromBytes(e.minBytes)
    }

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