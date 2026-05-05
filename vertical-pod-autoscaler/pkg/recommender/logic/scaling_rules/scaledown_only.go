package scaling_rules

import (
	logictypes "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/types"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/klog/v2"
)

type ScaleDownOnlyRule struct{}

func (r *ScaleDownOnlyRule) Apply(rec logictypes.RecommendedContainerResources, ctx logictypes.ScalingRuleContext) logictypes.RecommendedContainerResources {
	currentCPU := model.CPUAmountFromCores(ctx.CurrentCPURequest)
	currentMemory := model.MemoryAmountFromBytes(ctx.CurrentMemoryRequest)

	if rec.Target[model.ResourceCPU] > currentCPU {
		klog.V(4).InfoS("ScaleDownOnly: bloqueando scale-up de CPU",
			"current", ctx.CurrentCPURequest,
			"recommended", rec.Target[model.ResourceCPU],
		)
		rec.Target[model.ResourceCPU] = currentCPU
		rec.UpperBound[model.ResourceCPU] = currentCPU
	}

	if rec.Target[model.ResourceMemory] > currentMemory {
		klog.V(4).InfoS("ScaleDownOnly: bloqueando scale-up de memory",
			"current", ctx.CurrentMemoryRequest,
			"recommended", rec.Target[model.ResourceMemory],
		)
		rec.Target[model.ResourceMemory] = currentMemory
		rec.UpperBound[model.ResourceMemory] = currentMemory
	}

	return rec
}