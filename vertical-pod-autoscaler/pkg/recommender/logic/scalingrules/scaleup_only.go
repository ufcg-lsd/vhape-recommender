package scalingrules

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/klog/v2"
)

type ScaleUpOnlyRule struct{}

func (r *ScaleUpOnlyRule) Apply(rec recommendation.ResourceRecommendation, ctx ScalingRuleContext) recommendation.ResourceRecommendation {
	currentCPU, hasCPU := ctx.CurrentRequest[model.ResourceCPU]	
	currentMemory, hasMem := ctx.CurrentRequest[model.ResourceMemory]

	if hasCPU && rec.Target[model.ResourceCPU] < currentCPU {
		klog.V(4).InfoS("ScaleUpOnly: blocking CPU scale-down.",
			"current", currentCPU,
			"recommended", rec.Target[model.ResourceCPU],
		)
		rec.Target[model.ResourceCPU] = currentCPU
		rec.LowerBound[model.ResourceCPU] = currentCPU
	}

	if hasMem && rec.Target[model.ResourceMemory] < currentMemory {
		klog.V(4).InfoS("ScaleUpOnly: blocking memory scale-down.",
			"current", currentMemory,
			"recommended", rec.Target[model.ResourceMemory],
		)
		rec.Target[model.ResourceMemory] = currentMemory
		rec.LowerBound[model.ResourceMemory] = currentMemory
	}

	return rec
}