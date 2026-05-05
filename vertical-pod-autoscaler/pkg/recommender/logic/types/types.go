package types

import "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"

// RecommendedContainerResources is the recommendation for a container.
type RecommendedContainerResources struct {
	Target     model.Resources
	LowerBound model.Resources
	UpperBound model.Resources
}

// ScalingRuleContext carries all information a scaling rule needs.
type ScalingRuleContext struct {
	ContainerName        string
	Namespace            string
	CurrentCPURequest    float64
	CurrentMemoryRequest float64
}