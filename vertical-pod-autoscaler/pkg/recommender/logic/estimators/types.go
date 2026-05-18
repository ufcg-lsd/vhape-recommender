package estimators

import "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"

type ResourceRecommendation struct {
	Target         model.Resource
	LowerBound     model.Resource
	UpperBound     model.Resource
	UncappedTarget model.Resource
}

type ResourceConstraints struct {
	Min model.Resource
	Max model.Resource
}

type ResourceEstimator interface {
	FeedSamples(containerName string, samples []model.Resource)
	GetResourceRecommendation(containerName string, constraints ResourceConstraints) ResourceRecommendation
}