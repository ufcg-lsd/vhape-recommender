package estimators

import "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"

type ResourceRecommendation struct {
	Target         model.ResourceAmount
	LowerBound     model.ResourceAmount
	UpperBound     model.ResourceAmount
	UncappedTarget model.ResourceAmount
}

type ResourceConstraints struct {
	Min model.ResourceAmount
	Max model.ResourceAmount
}

type ResourceEstimator interface {
	FeedSamples(containerName string, samples []model.ResourceAmount)
	GetResourceRecommendation(containerName string, constraints ResourceConstraints) ResourceRecommendation
}