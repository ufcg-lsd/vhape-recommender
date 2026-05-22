package scalingrules

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/klog/v2"
)

type ScaleDownOnlyRule struct{}

func (r *ScaleDownOnlyRule) Apply(rec recommendation.ResourceRecommendation, ctx ScalingRuleContext) recommendation.ResourceRecommendation {
	if ctx.CurrentRequest == nil {
		return rec
	}

	currentCPU, hasCPU := ctx.CurrentRequest[model.ResourceCPU]	
	currentMemory, hasMem := ctx.CurrentRequest[model.ResourceMemory]

	if hasCPU && rec.Target[model.ResourceCPU] > currentCPU {
		klog.V(4).InfoS("ScaleDownOnly: blocking CPU scale-up",
			"current", currentCPU,
			"recommended", rec.Target[model.ResourceCPU],
		)
		rec.Target[model.ResourceCPU] = currentCPU
		rec.UpperBound[model.ResourceCPU] = currentCPU
	}

	if hasMem && rec.Target[model.ResourceMemory] > currentMemory {
		klog.V(4).InfoS("ScaleDownOnly: blocking memory scale-up",
			"current", currentMemory,
			"recommended", rec.Target[model.ResourceMemory],
		)
		rec.Target[model.ResourceMemory] = currentMemory
		rec.UpperBound[model.ResourceMemory] = currentMemory
	}

	return rec
}