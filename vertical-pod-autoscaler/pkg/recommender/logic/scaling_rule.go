package logic

import (
	logictypes "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/types"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/scaling_rules"
)

// ScalingRule is a business rule that can modify or block a recommendation.
type ScalingRule interface {
	Apply(recommendation logictypes.RecommendedContainerResources, ctx logictypes.ScalingRuleContext) logictypes.RecommendedContainerResources
}

// scaling rule names
const (
	ScalingRuleScaleDownOnly = "scale-down-only"
	ScalingRuleScaleUpOnly   = "scale-up-only"
)

// selectScalingRule returns the active scaling rule based on the VhapePolicy.
func selectScalingRule(policy *VhapePolicy) ScalingRule {
	switch policy.Spec.ScalingRule {
	case ScalingRuleScaleDownOnly:
		return &scaling_rules.ScaleDownOnlyRule{}
	case ScalingRuleScaleUpOnly:
		return &scaling_rules.ScaleUpOnlyRule{}
	default:
		return nil
	}
}