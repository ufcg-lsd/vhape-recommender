package recommendation

import "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"

type SingleResourceRecommendation struct {
	Target         model.ResourceAmount
	LowerBound     model.ResourceAmount
	UpperBound     model.ResourceAmount
	UncappedTarget model.ResourceAmount
}

type ResourceRecommendation struct {
	Target         model.Resources
	LowerBound     model.Resources
	UpperBound     model.Resources
	UncappedTarget model.Resources
}