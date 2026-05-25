/*
Package recommendation defines recommendation value types shared by the
recommender, estimators, and scaling rules.

This package serves to avoid import cycles.
*/

package recommendation

import "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"


// SingleResourceRecommendation represents the recommendation calculated for a
// single resource, such as CPU or memory.
//
// Estimators return this type because each estimator is responsible for one
// resource at a time.
type SingleResourceRecommendation struct {
	Target         model.ResourceAmount
	LowerBound     model.ResourceAmount
	UpperBound     model.ResourceAmount
	UncappedTarget model.ResourceAmount
}


// ResourceRecommendation represents the recommendation for all controlled
// resources of a container.
type ResourceRecommendation struct {
	Target         model.Resources
	LowerBound     model.Resources
	UpperBound     model.Resources
	UncappedTarget model.Resources
}