/*
This file defines the ResourceEstimator interface used by logic/recommender.go.

A ResourceEstimator is responsible for ingesting usage samples and producing
recommendations for a single resource, such as CPU or memory.

Implementations should provide the following methods:
  - FeedSamples: periodically receives usage metrics for a container.
  - GetSingleResourceRecommendation: returns a recommendation for one resource,
    taking the current resource constraints and history into account.

An estimator that needs more than the usage values (the pod each sample comes
from, the time of the measurement...) also implements ContainerSampleEstimator.
It then receives every sample as a ContainerSample, with all that is known about
it, instead of FeedSamples; it uses the fields it needs and ignores the others.
Estimators that only implement ResourceEstimator keep receiving FeedSamples.

This architecture differs from the original VPA recommender design by allowing
each estimator to define how usage samples are stored and processed. Moreover, 
since GetSingleResourceRecommendation returns a recommendation for a single resource,
different resources may also be handled by different estimator implementations.
*/

package estimators

import (
	"time"

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

// ContainerSample is one usage sample of a container, with everything the
// recommender knows about it. New information is added as new fields, so the
// estimators that already use it are not affected.
type ContainerSample struct {
	// Pod is the pod the container runs in.
	Pod model.PodID
	// MeasureStart is the time of the measurement, as reported by the metrics
	// API (the MeasureStart of the samples in the upstream VPA).
	MeasureStart time.Time
	// Window is the length of the measurement interval, which ends at
	// MeasureStart.
	Window time.Duration
	// Usage is the measured usage of the resource.
	Usage model.ResourceAmount
}

// ContainerSampleEstimator is optionally implemented by a ResourceEstimator that
// needs the full samples. The recommender calls FeedContainerSamples instead of
// FeedSamples for such an estimator, with the samples in the same order.
type ContainerSampleEstimator interface {
	ResourceEstimator
	FeedContainerSamples(containerName string, samples []ContainerSample)
}
