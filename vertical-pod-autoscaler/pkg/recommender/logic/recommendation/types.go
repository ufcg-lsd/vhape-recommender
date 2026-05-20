package recommendation

import "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"

type ResourceRecommendation struct {
	Target         model.ResourceAmount
	LowerBound     model.ResourceAmount
	UpperBound     model.ResourceAmount
	UncappedTarget model.ResourceAmount
}