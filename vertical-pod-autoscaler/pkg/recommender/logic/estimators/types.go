package estimators

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
)


type ResourceConstraints struct {
	Min model.ResourceAmount
	Max model.ResourceAmount
}

type ResourceEstimator interface {
	FeedSamples(containerName string, samples []model.ResourceAmount)
	GetSingleResourceRecommendation(containerName string, constraints ResourceConstraints) recommendation.SingleResourceRecommendation
}
