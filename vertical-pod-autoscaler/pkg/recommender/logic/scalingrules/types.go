package scalingrules

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const (
	ScalingRuleScaleDownOnly = "scale-down-only"
	ScalingRuleScaleUpOnly   = "scale-up-only"
)

type ScalingRuleContext struct {
	ContainerName string
	Current       model.Resources
}

type ScalingRule interface {
	Apply(recommendation recommendation.ResourceRecommendation, ctx ScalingRuleContext) recommendation.ResourceRecommendation
}

// selectScalingRule returns the active scaling rule based on the VhapePolicy.
func selectScalingRule(name string) ScalingRule {
	switch name {
		case ScalingRuleScaleDownOnly:
			return &ScaleDownOnlyRule{}
		case ScalingRuleScaleUpOnly:
			return &ScaleUpOnlyRule{}
		default:
			return nil
	}
}