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
	"sort"
	"time"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	logictypes "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/types"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
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

// podResourceRecommender computes resource recommendation for each container.
type podResourceRecommender struct {
	dynamicClient dynamic.Interface
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
	policy, _ := FetchVhapePolicy(r.dynamicClient, namespace, policyName)
	for containerName, aggregatedContainerState := range containerNameToAggregateStateMap {
		recommendation[containerName] = r.estimateContainerResources(aggregatedContainerState, containerName, policy)
	}
	return recommendation
}

func (r *podResourceRecommender) estimateContainerResources(s *model.AggregateContainerState, containerName string, policy *VhapePolicy) logictypes.RecommendedContainerResources {
	resources := s.GetControlledResources()

	cpuEstimator, memEstimator := selectHeuristic(policy)

	targetCPUVal := cpuEstimator.GetCPUEstimation(s, containerName)
	targetMemVal := memEstimator.GetMemoryEstimation(s, containerName)

	cpuHeadroom := policy.Spec.CPU.Headroom
	memHeadroom := policy.Spec.Memory.Headroom

	target := model.Resources{
		model.ResourceCPU:    targetCPUVal,
		model.ResourceMemory: targetMemVal,
	}
	lowerBound := model.Resources{
		model.ResourceCPU:    model.ScaleResource(targetCPUVal, 1-cpuHeadroom),
		model.ResourceMemory: model.ScaleResource(targetMemVal, 1-memHeadroom),
	}
	upperBound := model.Resources{
		model.ResourceCPU:    model.ScaleResource(targetCPUVal, 1+cpuHeadroom),
		model.ResourceMemory: model.ScaleResource(targetMemVal, 1+memHeadroom),
	}

	rec := logictypes.RecommendedContainerResources{
		Target:     FilterControlledResources(target, resources),
		LowerBound: FilterControlledResources(lowerBound, resources),
		UpperBound: FilterControlledResources(upperBound, resources),
	}

	rule := selectScalingRule(policy)
	if rule != nil {
		ctx := logictypes.ScalingRuleContext{
			ContainerName: containerName,
		}
		rec = rule.Apply(rec, ctx)
	}

	klog.V(4).InfoS("estimateContainerResources resultado",
		"containerName", containerName,
		"targetCPUCores", float64(targetCPUVal)/1000,
		"targetCPUMillicores", targetCPUVal,
		"lowerCPUMillicores", float64(lowerBound[model.ResourceCPU]),
		"upperCPUMillicores", float64(upperBound[model.ResourceCPU]),
		"targetMemBytes", targetMemVal,
		"targetMemMB", float64(targetMemVal)/1024/1024,
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
func CreatePodResourceRecommender(config RecommendationConfig, dynamicClient dynamic.Interface) PodResourceRecommender {
	return &podResourceRecommender{
		dynamicClient: dynamicClient,
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
