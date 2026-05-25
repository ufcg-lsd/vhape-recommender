/*
This file defines the ResourceEstimator interface used by logic/recommender.go.

A ResourceEstimator is responsible for ingesting usage samples and producing
recommendations for a single resource, such as CPU or memory.

Implementations should provide the following methods:
  - FeedSamples: periodically receives usage metrics for a container.
  - GetSingleResourceRecommendation: returns a recommendation for one resource,
    taking the current resource constraints and history into account.

This architecture differs from the original VPA recommender design by allowing
each estimator to define how usage samples are stored and processed. Moreover, 
since GetSingleResourceRecommendation returns a recommendation for a single resource,
different resources may also be handled by different estimator implementations.
*/

package estimators

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

// ContainerResourceConstraints describes the current request and the allowed
// recommendation bounds for a container resource.
//
// CurrentRequest is included because estimators may use it as a fallback when
// they cannot produce a recommendation from usage history.
type ContainerResourceConstraints struct {
	CurrentRequest model.ResourceAmount
	Min   		   model.ResourceAmount
	Max 		   model.ResourceAmount
}

type ResourceEstimator interface {
	FeedSamples(containerName string, samples []model.ResourceAmount)
	GetSingleResourceRecommendation(containerName string, constraints ContainerResourceConstraints) recommendation.SingleResourceRecommendation
}
