package logic

import (
	"sort"
	"sync"
	"context"
	
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	input_metrics "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/metrics"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/estimators"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/scalingrules"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

// PodResourceRecommender computes resource recommendation for a Vpa object.
type PodResourceRecommender interface {
	GetRecommendedPodResources(
		containerNameToAggregateStateMap model.ContainerNameToAggregateStateMap,
		namespace string,
		annotations map[string]string,
		matchingPods []model.PodID,
	) RecommendedPodResources
}

type podResourceRecommender struct {
	config        RecommendationLimits
	dynamicClient dynamic.Interface
	metricsClient input_metrics.MetricsClient
	mu            sync.Mutex
	estimators    map[string]*ResourceEstimators
}

// CreatePodResourceRecommender returns the primary recommender.
func CreatePodResourceRecommender(config RecommendationLimits, dynamicClient dynamic.Interface, metricsClient input_metrics.MetricsClient) PodResourceRecommender {
	return &podResourceRecommender{
		config:        config,
		dynamicClient: dynamicClient,
		metricsClient: metricsClient,
		estimators:    make(map[string]*ResourceEstimators),
	}
}

type RecommendationLimits struct {
	PodMinCPUMillicores float64
	PodMinMemoryMb      float64
}

// RecommendationFormat controls how numeric values are rendered in outputs.
type RecommendationFormat struct {
	HumanizeMemory     bool
	RoundCPUMillicores int
	RoundMemoryBytes   int
}

// RecommendedPodResources is a Map from container name to recommended resources.
type RecommendedPodResources map[string]recommendation.ResourceRecommendation

type ResourceEstimators struct {
	CPU    estimators.ResourceEstimator
	Memory estimators.ResourceEstimator
}

type containerUsage struct {
	CPU    map[string][]model.ResourceAmount
	Memory map[string][]model.ResourceAmount
}

func (r *podResourceRecommender) GetRecommendedPodResources(
	containerStates model.ContainerNameToAggregateStateMap,
	namespace string,
	annotations map[string]string,
	matchingPods []model.PodID,
) RecommendedPodResources {
	recommendations := make(RecommendedPodResources)

	if len(containerStates) == 0 {
		return recommendations
	}

	policy, ok := r.fetchPolicy(namespace, annotations["vhape/policy"])
	if !ok {
		return recommendations
	}

	usage := r.collectCurrentUsage(namespace, matchingPods)
	estimators := r.getOrCreateEstimators(policy)
	for containerName := range containerStates {
		estimators.CPU.FeedSamples(containerName, usage.CPU[containerName])
		estimators.Memory.FeedSamples(containerName, usage.Memory[containerName])
	}

	for containerName, state := range containerStates {
		recommendations[containerName] = r.recommendContainerResources(
			containerName,
			state,
			policy,
			estimators,
			len(containerStates),
		)
	}

	return recommendations
}

func (r *podResourceRecommender) fetchPolicy(namespace string, policyName string) (*VhapePolicy, bool) {
	policy, err := FetchVhapePolicy(r.dynamicClient, namespace, policyName)
	if err != nil {
		klog.Warningf("Skipping VPA in namespace %q: %v", namespace, err)
		return nil, false
	}

	return policy, true
}

func (r *podResourceRecommender) collectCurrentUsage(namespace string, matchingPods []model.PodID) containerUsage {
	usage := containerUsage{
		CPU:    make(map[string][]model.ResourceAmount),
		Memory: make(map[string][]model.ResourceAmount),
	}

	podSet := make(map[model.PodID]struct{}, len(matchingPods))
	for _, podID := range matchingPods {
		podSet[podID] = struct{}{}
	}

	snapshots, err := r.metricsClient.GetContainersMetrics(context.TODO())
	if err != nil {
		klog.Warningf("Failed to collect metrics in namespace %q: %v", namespace, err)
		return usage
	}

	for _, snap := range snapshots {
		if snap.ID.PodID.Namespace != namespace {
			continue
		}
		
		if _, ok := podSet[snap.ID.PodID]; !ok {
			continue
		}

		containerName := snap.ID.ContainerName

		usage.CPU[containerName] = append(
			usage.CPU[containerName],
			snap.Usage[model.ResourceCPU],
		)

		usage.Memory[containerName] = append(
			usage.Memory[containerName],
			snap.Usage[model.ResourceMemory],
		)
	}

	return usage
}

func (r *podResourceRecommender) getOrCreateEstimators(policy *VhapePolicy) *ResourceEstimators {
	key := policy.Namespace + "/" + policy.Name

	r.mu.Lock()
	defer r.mu.Unlock()

	if cached, ok := r.estimators[key]; ok {
		klog.V(4).InfoS("Cached estimators found for the policy", "key", key)
		return cached
	}

	created := &ResourceEstimators{
		CPU:    policy.Spec.Resources.CPU.Heuristic.NewEstimator(model.ResourceCPU),
		Memory: policy.Spec.Resources.Memory.Heuristic.NewEstimator(model.ResourceMemory),
	}

	r.estimators[key] = created

	return created
}

func (r *podResourceRecommender) recommendContainerResources(
	containerName string,
	state *model.AggregateContainerState,
	policy *VhapePolicy,
	est *ResourceEstimators,
	containerCount int,
) recommendation.ResourceRecommendation {
	controlledResources := state.GetControlledResources()

	cpuRec := est.CPU.GetSingleResourceRecommendation(
		containerName,
		r.calculateContainerCpuConstraints(containerCount),
	)

	memRec := est.Memory.GetSingleResourceRecommendation(
		containerName,
		r.calculateContainerMemoryConstraints(containerCount),
	)

	rec := recommendation.ResourceRecommendation{
		Target: FilterControlledResources(model.Resources{
			model.ResourceCPU:    cpuRec.Target,
			model.ResourceMemory: memRec.Target,
		}, controlledResources),

		LowerBound: FilterControlledResources(model.Resources{
			model.ResourceCPU:    cpuRec.LowerBound,
			model.ResourceMemory: memRec.LowerBound,
		}, controlledResources),

		UpperBound: FilterControlledResources(model.Resources{
			model.ResourceCPU:    cpuRec.UpperBound,
			model.ResourceMemory: memRec.UpperBound,
		}, controlledResources),

		UncappedTarget: FilterControlledResources(model.Resources{
			model.ResourceCPU:    cpuRec.UncappedTarget,
			model.ResourceMemory: memRec.UncappedTarget,
		}, controlledResources),
	}

	rec = r.applyScalingRule(rec, policy, containerName, state)

	klog.V(4).InfoS("Estimated container resources",
		"containerName", containerName,
		"targetCPUMillicores", rec.Target[model.ResourceCPU],
		"lowerCPUMillicores", rec.LowerBound[model.ResourceCPU],
		"upperCPUMillicores", rec.UpperBound[model.ResourceCPU],
		"targetMemMB", float64(rec.Target[model.ResourceMemory])/1024/1024,
		"lowerMemMB", float64(rec.LowerBound[model.ResourceMemory])/1024/1024,
		"upperMemMB", float64(rec.UpperBound[model.ResourceMemory])/1024/1024,
	)

	return rec
}

func (r *podResourceRecommender) calculateContainerCpuConstraints(containerCount int) estimators.ContainerResourceConstraints {
	if containerCount <= 0 {
		return estimators.ContainerResourceConstraints{}
	}

	minCPU := model.ResourceAmount(r.config.PodMinCPUMillicores / float64(containerCount))

	return estimators.ContainerResourceConstraints{
		Min: minCPU,
	}
}

func (r *podResourceRecommender) calculateContainerMemoryConstraints(containerCount int) estimators.ContainerResourceConstraints {
	if containerCount <= 0 {
		return estimators.ContainerResourceConstraints{}
	}

	minMemory := model.ResourceAmount(
		(r.config.PodMinMemoryMb * 1024 * 1024) / float64(containerCount),
	)

	return estimators.ContainerResourceConstraints{
		Min: minMemory,
	}
}

func (r *podResourceRecommender) applyScalingRule(
	rec recommendation.ResourceRecommendation,
	policy *VhapePolicy,
	containerName string,
	state *model.AggregateContainerState,
) recommendation.ResourceRecommendation {
	rule := scalingrules.SelectScalingRule(policy.Spec.ScalingRule)
	if rule == nil {
		return rec
	}

	return rule.Apply(rec, scalingrules.ScalingRuleContext{
		ContainerName:  containerName,
		CurrentRequest: state.GetLastObservedRequest(),
	})
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