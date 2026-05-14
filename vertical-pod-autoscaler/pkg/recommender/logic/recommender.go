package logic

import (
	"sort"
	"sync"
	"time"
	"context"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	logictypes "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/types"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	input_metrics "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/metrics"
)

type RecommendationConfig struct {
	SafetyMarginFraction       float64
	PodMinCPUMillicores        float64
	PodMinMemoryMb             float64
	TargetCPUPercentile        float64
	LowerBoundCPUPercentile    float64
	UpperBoundCPUPercentile    float64
	ConfidenceIntervalCPU      time.Duration
	TargetMemoryPercentile     float64
	LowerBoundMemoryPercentile float64
	UpperBoundMemoryPercentile float64
	ConfidenceIntervalMemory   time.Duration
}

// RecommendationFormat controls how numeric values are rendered in outputs.
type RecommendationFormat struct {
	HumanizeMemory     bool
	RoundCPUMillicores int
	RoundMemoryBytes   int
}

// PodResourceRecommender computes resource recommendation for a Vpa object.
type PodResourceRecommender interface {
	GetRecommendedPodResources(
		containerNameToAggregateStateMap model.ContainerNameToAggregateStateMap,
		namespace string,
		annotations map[string]string,
	) RecommendedPodResources
}

// RecommendedContainerResources is an alias for logictypes.RecommendedContainerResources.
type RecommendedContainerResources = logictypes.RecommendedContainerResources

// RecommendedPodResources is a Map from container name to recommended resources.
type RecommendedPodResources map[string]RecommendedContainerResources

// cachedEstimators holds the estimators for a given policy — persists between cycles.
type cachedEstimators struct {
	cpu    CPUEstimator
	memory MemoryEstimator
}

// podResourceRecommender computes resource recommendation for each container.
type podResourceRecommender struct {
    dynamicClient dynamic.Interface
    metricsClient input_metrics.MetricsClient
    mu            sync.Mutex
    estimators    map[string]*cachedEstimators
}

func (r *podResourceRecommender) getOrCreateEstimators(policy *VhapePolicy) (CPUEstimator, MemoryEstimator) {
	key := policy.Namespace + "/" + policy.Name
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.estimators[key]; ok {
		return cached.cpu, cached.memory
	}
	cpu, mem := selectHeuristic(policy)
	r.estimators[key] = &cachedEstimators{cpu: cpu, memory: mem}
	klog.V(4).InfoS("Criando novos estimadores para policy", "key", key, "heuristic", policy.Spec.Heuristic)
	return cpu, mem
}

func (r *podResourceRecommender) GetRecommendedPodResources(
    containerNameToAggregateStateMap model.ContainerNameToAggregateStateMap,
    namespace string,
    annotations map[string]string,
) RecommendedPodResources {
    var recommendation = make(RecommendedPodResources)
    if len(containerNameToAggregateStateMap) == 0 {
        return recommendation
    }
    policyName := annotations["vhape/policy"]
    policy, err := FetchVhapePolicy(r.dynamicClient, namespace, policyName)
    if err != nil {
        klog.Warningf("Skipping VPA in namespace %q: %v", namespace, err)
        return recommendation
    }

    // coleta métricas atuais uma vez por ciclo
    snapshots, err := r.metricsClient.GetContainersMetrics(context.TODO())
    if err != nil {
        klog.Warningf("Falha ao coletar métricas em namespace %q: %v", namespace, err)
        snapshots = nil
    }

    // monta mapa containerName → slice de amostras (uma por pod)
    cpuUsageMap := make(map[string][]float64)
    memUsageMap := make(map[string][]float64)
    for _, snap := range snapshots {
        if snap.ID.PodID.Namespace == namespace {
            name := snap.ID.ContainerName
            cpuUsageMap[name] = append(cpuUsageMap[name], float64(snap.Usage[model.ResourceCPU]))
            memUsageMap[name] = append(memUsageMap[name], float64(snap.Usage[model.ResourceMemory]))
        }
    }

    for containerName, aggregatedContainerState := range containerNameToAggregateStateMap {
        recommendation[containerName] = r.estimateContainerResources(
            aggregatedContainerState,
            containerName,
            policy,
            cpuUsageMap[containerName],
            memUsageMap[containerName],
        )
    }
    return recommendation
}

func (r *podResourceRecommender) estimateContainerResources(s *model.AggregateContainerState, containerName string, policy *VhapePolicy, currentCPUs []float64, currentMemories []float64) logictypes.RecommendedContainerResources {
	resources := s.GetControlledResources()

	cpuEstimator, memEstimator := r.getOrCreateEstimators(policy)

	p93CPU := cpuEstimator.GetCPUEstimation(s, containerName, currentCPUs)
	p93Mem := memEstimator.GetMemoryEstimation(s, containerName, currentMemories)

	cpuH := 1 + policy.Spec.CPU.Headroom
	memH := 1 + policy.Spec.Memory.Headroom

	target := model.Resources{
		model.ResourceCPU:    model.ScaleResource(p93CPU, cpuH),
		model.ResourceMemory: model.ScaleResource(p93Mem, memH),
	}
	lowerBound := model.Resources{
		model.ResourceCPU:    model.ScaleResource(p93CPU, (1-policy.Spec.CPU.LowerBound)*cpuH),
		model.ResourceMemory: model.ScaleResource(p93Mem, (1-policy.Spec.Memory.LowerBound)*memH),
	}
	upperBound := model.Resources{
		model.ResourceCPU:    model.ScaleResource(p93CPU, (1+policy.Spec.CPU.UpperBound)*cpuH),
		model.ResourceMemory: model.ScaleResource(p93Mem, (1+policy.Spec.Memory.UpperBound)*memH),
	}

	rec := logictypes.RecommendedContainerResources{
		Target:     FilterControlledResources(target, resources),
		LowerBound: FilterControlledResources(lowerBound, resources),
		UpperBound: FilterControlledResources(upperBound, resources),
	}

	rule := selectScalingRule(policy)
	if rule != nil {
		lastRec := s.GetLastRecommendation()
		cpuRequest := 0.0
		memRequest := 0.0
		if lastRec != nil {
			if q := lastRec.Cpu(); q != nil {
				cpuRequest = float64(q.MilliValue()) / 1000.0
			}
			if q := lastRec.Memory(); q != nil {
				memRequest = float64(q.Value())
			}
		}
		ctx := logictypes.ScalingRuleContext{
			ContainerName:        containerName,
			CurrentCPURequest:    cpuRequest,
			CurrentMemoryRequest: memRequest,
		}
		rec = rule.Apply(rec, ctx)
	}

	klog.V(4).InfoS("estimateContainerResources resultado",
		"containerName", containerName,
		"p93CPUCores", float64(p93CPU)/1000,
		"targetCPUMillicores", float64(target[model.ResourceCPU]),
		"lowerCPUMillicores", float64(lowerBound[model.ResourceCPU]),
		"upperCPUMillicores", float64(upperBound[model.ResourceCPU]),
		"p93MemBytes", p93Mem,
		"targetMemMB", float64(target[model.ResourceMemory])/1024/1024,
		"lowerMemMB", float64(lowerBound[model.ResourceMemory])/1024/1024,
		"upperMemMB", float64(upperBound[model.ResourceMemory])/1024/1024,
	)

	return rec
}

// FilterControlledResources returns estimations from 'estimation' only for resources present in 'controlledResources'.
func FilterControlledResources(estimation model.Resources, controlledResources []model.ResourceName) model.Resources {
	result := make(model.Resources)
	for _, resource := range controlledResources {
		if value, ok := estimation[resource]; ok {
			result[resource] = value
		}
	}
	return result
}

// CreatePodResourceRecommender returns the primary recommender.
func CreatePodResourceRecommender(config RecommendationConfig, dynamicClient dynamic.Interface, metricsClient input_metrics.MetricsClient) PodResourceRecommender {
    return &podResourceRecommender{
        dynamicClient: dynamicClient,
        metricsClient: metricsClient,
        estimators:    make(map[string]*cachedEstimators),
    }
}

// MapToListOfRecommendedContainerResources converts the map into a stable sorted list.
func MapToListOfRecommendedContainerResources(resources RecommendedPodResources, format RecommendationFormat) *vpa_types.RecommendedPodResources {
	containerResources := make([]vpa_types.RecommendedContainerResources, 0, len(resources))
	containerNames := make([]string, 0, len(resources))
	for containerName := range resources {
		containerNames = append(containerNames, containerName)
	}
	sort.Strings(containerNames)
	for _, name := range containerNames {
		containerResources = append(containerResources, vpa_types.RecommendedContainerResources{
			ContainerName:  name,
			Target:         model.ResourcesAsResourceList(resources[name].Target, format.HumanizeMemory, format.RoundCPUMillicores, format.RoundMemoryBytes),
			LowerBound:     model.ResourcesAsResourceList(resources[name].LowerBound, format.HumanizeMemory, format.RoundCPUMillicores, format.RoundMemoryBytes),
			UpperBound:     model.ResourcesAsResourceList(resources[name].UpperBound, format.HumanizeMemory, format.RoundCPUMillicores, format.RoundMemoryBytes),
			UncappedTarget: model.ResourcesAsResourceList(resources[name].Target, format.HumanizeMemory, format.RoundCPUMillicores, format.RoundMemoryBytes),
		})
	}
	return &vpa_types.RecommendedPodResources{
		ContainerRecommendations: containerResources,
	}
}