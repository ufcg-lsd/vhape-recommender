package scalingrules

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const (
	BlockScaleUpRule   = "block-scale-up"
	BlockScaleDownRule = "block-scale-down"
)


// ScalingRule adjusts a single-resource recommendation according to a specific
// scaling policy.
//
// Implementations may clamp, preserve, or adjust the recommendation
// based on the current request and the selected scaling behavior.
type ScalingRule interface {
	Apply(
		resourceRecommendation recommendation.SingleResourceRecommendation,
		containerName string,
		resourceName model.ResourceName,
		currentRequest model.ResourceAmount,
	) recommendation.SingleResourceRecommendation
}

// SelectScalingRule returns the scaling rule associated with the given scaling rule name.
func SelectScalingRule(name string) ScalingRule {
	switch name {
	case BlockScaleUpRule:
		return &BlockScaleUp{}
	case BlockScaleDownRule:
		return &BlockScaleDown{}
	default:
		return nil
	}
}