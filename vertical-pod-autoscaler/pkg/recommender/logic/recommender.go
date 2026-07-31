package logic

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	input_metrics "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/metrics"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/estimators"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/scalingrules"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

const vhapePolicyAnnotation = "vhape/policy"

// PodResourceRecommender computes resource recommendations for a VPA object.
type PodResourceRecommender interface {
	GetRecommendedPodResources(
		containerNameToAggregateStateMap model.ContainerNameToAggregateStateMap,
		vpa *model.Vpa,
		matchingPods []model.PodID,
	) (RecommendedPodResources, error)
}

// PodRecommendationLimits contains global pod minimum recommendation limits.
//
// The values are defined at pod level and are divided by the number of
// containers when calculating per-container constraints. Follows the original VPA logic.
type PodRecommendationLimits struct {
	PodMinCPUMillicores float64
	PodMinMemoryMb      float64
}

// RecommendationFormat controls how numeric values are rendered in outputs.
type RecommendationFormat struct {
	HumanizeMemory     bool
	RoundCPUMillicores int
	RoundMemoryBytes   int
}

// RecommendedPodResources maps container names to their resource recommendations.
type RecommendedPodResources map[string]recommendation.ResourceRecommendation

// ResourceEstimators contains the estimators used for each supported resource.
//
// Each estimator is resource-specific. This allows CPU and memory to use
// different heuristic implementations or different heuristic configurations.
type ResourceEstimators struct {
	CPU    estimators.ResourceEstimator
	Memory estimators.ResourceEstimator

	policyNamespace string
	policyName      string
}

// podResourceRecommender is the default PodResourceRecommender implementation.
//
// It fetches VhapePolicy objects, collects current usage metrics, feeds
// resource estimators, and converts estimator outputs into VPA-compatible
// recommendations.
type podResourceRecommender struct {
	config        PodRecommendationLimits
	dynamicClient dynamic.Interface
	metricsClient input_metrics.MetricsClient
	mu            sync.Mutex

	// estimators caches resource estimators by VPA.
	// Estimators keep usage history in memory, so they must be reused across
	// recommendation cycles for the same VPA.
	estimators map[model.VpaID]*ResourceEstimators
}

// containerUsage stores current resource usage samples grouped by container name.
type containerUsage struct {
	CPU    map[string][]model.ResourceAmount
	Memory map[string][]model.ResourceAmount
}

// CreatePodResourceRecommender returns the primary recommender.
func CreatePodResourceRecommender(config PodRecommendationLimits, dynamicClient dynamic.Interface, metricsClient input_metrics.MetricsClient) PodResourceRecommender {
	return &podResourceRecommender{
		config:        config,
		dynamicClient: dynamicClient,
		metricsClient: metricsClient,
		estimators:    make(map[model.VpaID]*ResourceEstimators),
	}
}

// GetRecommendedPodResources returns recommendations for all containers managed by a VPA.
func (r *podResourceRecommender) GetRecommendedPodResources(
	containerStates model.ContainerNameToAggregateStateMap,
	vpa *model.Vpa,
	matchingPods []model.PodID,
) (RecommendedPodResources, error) {
	if vpa == nil {
		return nil, fmt.Errorf("VPA is nil")
	}

	policy, err := r.fetchPolicy(vpa)
	if err != nil {
		return nil, err
	}

	resourceEstimators := r.getOrCreateEstimators(vpa, policy)
	recommendations := make(RecommendedPodResources)
	if len(containerStates) == 0 {
		return recommendations, nil
	}

	usage, err := r.collectCurrentUsage(matchingPods)
	if err != nil {
		return nil, fmt.Errorf("VPA %q/%q: collect current usage: %w", vpa.ID.Namespace, vpa.ID.VpaName, err)
	}

	for containerName := range containerStates {
		if cpuSamples := usage.CPU[containerName]; len(cpuSamples) > 0 {
			resourceEstimators.CPU.FeedSamples(containerName, cpuSamples)
		}
		if memorySamples := usage.Memory[containerName]; len(memorySamples) > 0 {
			resourceEstimators.Memory.FeedSamples(containerName, memorySamples)
		}
	}

	for containerName, state := range containerStates {
		recommendations[containerName] = r.recommendContainerResources(
			containerName,
			state,
			policy,
			resourceEstimators,
			len(containerStates),
		)
	}

	return recommendations, nil
}

// fetchPolicy loads the VhapePolicy referenced by the VPA annotation.
func (r *podResourceRecommender) fetchPolicy(vpa *model.Vpa) (*VhapePolicy, error) {
	policyRef := vpa.Annotations[vhapePolicyAnnotation]
	parts := strings.Split(policyRef, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf(
			"VPA %q/%q: invalid or missing %s annotation; expected <namespace>/<name>",
			vpa.ID.Namespace,
			vpa.ID.VpaName,
			vhapePolicyAnnotation,
		)
	}

	policy, err := FetchVhapePolicy(r.dynamicClient, parts[0], parts[1])
	if err != nil {
		return nil, fmt.Errorf(
			"VPA %q/%q: fetch policy %q: %w",
			vpa.ID.Namespace,
			vpa.ID.VpaName,
			policyRef,
			err,
		)
	}

	return policy, nil
}

// collectCurrentUsage collects the latest usage metrics for containers running in the
// pods matched by the VPA.
func (r *podResourceRecommender) collectCurrentUsage(matchingPods []model.PodID) (containerUsage, error) {
	podSet := make(map[model.PodID]struct{}, len(matchingPods))
	for _, podID := range matchingPods {
		podSet[podID] = struct{}{}
	}

	snapshots, err := r.metricsClient.GetContainersMetrics(context.TODO())
	if err != nil {
		return containerUsage{}, fmt.Errorf("collect container metrics: %w", err)
	}

	usage := containerUsage{
		CPU:    make(map[string][]model.ResourceAmount),
		Memory: make(map[string][]model.ResourceAmount),
	}

	for _, snap := range snapshots {
		if _, ok := podSet[snap.ID.PodID]; !ok {
			continue
		}

		containerName := snap.ID.ContainerName
		if cpu, ok := snap.Usage[model.ResourceCPU]; ok {
			usage.CPU[containerName] = append(usage.CPU[containerName], cpu)
		}
		if memory, ok := snap.Usage[model.ResourceMemory]; ok {
			usage.Memory[containerName] = append(usage.Memory[containerName], memory)
		}
	}

	return usage, nil
}

// getOrCreateEstimators returns the estimators associated with a VPA.
// The estimators are recreated when the VPA annotation points to another policy.
func (r *podResourceRecommender) getOrCreateEstimators(vpa *model.Vpa, policy *VhapePolicy) *ResourceEstimators {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cached, ok := r.estimators[vpa.ID]; ok {
		if cached.policyNamespace == policy.Namespace && cached.policyName == policy.Name {
			klog.V(4).InfoS(
				"Cached estimators found",
				"vpa", klog.KRef(vpa.ID.Namespace, vpa.ID.VpaName),
				"policy", klog.KRef(policy.Namespace, policy.Name),
			)
			return cached
		}

		klog.InfoS(
			"VPA policy changed; estimators discarded",
			"vpa", klog.KRef(vpa.ID.Namespace, vpa.ID.VpaName),
			"oldPolicy", klog.KRef(cached.policyNamespace, cached.policyName),
			"newPolicy", klog.KRef(policy.Namespace, policy.Name),
		)
	}

	created := &ResourceEstimators{
		CPU:             policy.Spec.Resources.CPU.Heuristic.NewEstimator(model.ResourceCPU),
		Memory:          policy.Spec.Resources.Memory.Heuristic.NewEstimator(model.ResourceMemory),
		policyNamespace: policy.Namespace,
		policyName:      policy.Name,
	}

	r.estimators[vpa.ID] = created
	return created
}

// recommendContainerResources calculates a full resource recommendation for one container.
func (r *podResourceRecommender) recommendContainerResources(
	containerName string,
	state *model.AggregateContainerState,
	policy *VhapePolicy,
	est *ResourceEstimators,
	containerCount int,
) recommendation.ResourceRecommendation {
	controlledResources := state.GetControlledResources()
	observedRequest := state.GetLastObservedRequest()

	cpuRec := est.CPU.GetSingleResourceRecommendation(
		containerName,
		r.calculateContainerCpuConstraints(containerCount, observedRequest[model.ResourceCPU]),
	)

	cpuRec = r.applyScalingRule(
		cpuRec,
		policy,
		containerName,
		model.ResourceCPU,
		observedRequest[model.ResourceCPU],
	)

	memRec := est.Memory.GetSingleResourceRecommendation(
		containerName,
		r.calculateContainerMemoryConstraints(containerCount, observedRequest[model.ResourceMemory]),
	)
	memRec = r.applyScalingRule(
		memRec,
		policy,
		containerName,
		model.ResourceMemory,
		observedRequest[model.ResourceMemory],
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

	klog.V(4).InfoS("Estimated container resources",
		"containerName", containerName,
		"targetCPUMillicores", rec.Target[model.ResourceCPU],
		"lowerCPUMillicores", rec.LowerBound[model.ResourceCPU],
		"upperCPUMillicores", rec.UpperBound[model.ResourceCPU],
		"uncappedTargetCPUMillicores", rec.UncappedTarget[model.ResourceCPU],
		"targetMemMB", float64(rec.Target[model.ResourceMemory])/1024/1024,
		"lowerMemMB", float64(rec.LowerBound[model.ResourceMemory])/1024/1024,
		"upperMemMB", float64(rec.UpperBound[model.ResourceMemory])/1024/1024,
		"uncappedTargetMemMB", float64(rec.UncappedTarget[model.ResourceMemory])/1024/1024,
	)

	return rec
}

// calculateContainerCPUConstraints returns per-container CPU recommendation constraints.
func (r *podResourceRecommender) calculateContainerCpuConstraints(containerCount int, request model.ResourceAmount) estimators.ContainerResourceConstraints {
	if containerCount <= 0 {
		return estimators.ContainerResourceConstraints{}
	}

	minCPU := model.ResourceAmount(r.config.PodMinCPUMillicores / float64(containerCount))

	return estimators.ContainerResourceConstraints{
		Min:            minCPU,
		CurrentRequest: request,
	}
}

// calculateContainerMemoryConstraints returns per-container memory recommendation constraints.
func (r *podResourceRecommender) calculateContainerMemoryConstraints(containerCount int, request model.ResourceAmount) estimators.ContainerResourceConstraints {
	if containerCount <= 0 {
		return estimators.ContainerResourceConstraints{}
	}

	minMemory := model.ResourceAmount(
		(r.config.PodMinMemoryMb * 1024 * 1024) / float64(containerCount),
	)

	return estimators.ContainerResourceConstraints{
		Min:            minMemory,
		CurrentRequest: request,
	}
}

// applyScalingRule applies the policy scaling rule to a single-resource recommendation.
func (r *podResourceRecommender) applyScalingRule(
	rec recommendation.SingleResourceRecommendation,
	policy *VhapePolicy,
	containerName string,
	resourceName model.ResourceName,
	currentRequest model.ResourceAmount,
) recommendation.SingleResourceRecommendation {
	rule := scalingrules.SelectScalingRule(policy.Spec.ScalingRule)
	if rule == nil {
		return rec
	}

	return rule.Apply(rec, containerName, resourceName, currentRequest)
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
			UncappedTarget: model.ResourcesAsResourceList(resources[name].UncappedTarget, format.HumanizeMemory, format.RoundCPUMillicores, format.RoundMemoryBytes),
		})
	}
	return &vpa_types.RecommendedPodResources{
		ContainerRecommendations: containerResources,
	}
}
