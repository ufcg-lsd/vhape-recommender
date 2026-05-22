package estimators

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

type ContainerResourceConstraints struct {
	CurrentRequest model.ResourceAmount
	Min   		   model.ResourceAmount
	Max 		   model.ResourceAmount
}

type ResourceEstimator interface {
	FeedSamples(containerName string, samples []model.ResourceAmount)
	GetSingleResourceRecommendation(containerName string, constraints ContainerResourceConstraints) recommendation.SingleResourceRecommendation
}
