package scaling_rules

import (
	logictypes "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/types"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/klog/v2"
)

type ScaleUpOnlyRule struct{}

func (r *ScaleUpOnlyRule) Apply(rec logictypes.RecommendedContainerResources, ctx logictypes.ScalingRuleContext) logictypes.RecommendedContainerResources {
	currentCPU := model.CPUAmountFromCores(ctx.CurrentCPURequest)
	currentMemory := model.MemoryAmountFromBytes(ctx.CurrentMemoryRequest)

	if rec.Target[model.ResourceCPU] < currentCPU {
		klog.V(4).InfoS("ScaleUpOnly: bloqueando scale-down de CPU",
			"current", ctx.CurrentCPURequest,
			"recommended", rec.Target[model.ResourceCPU],
		)
		rec.Target[model.ResourceCPU] = currentCPU
		rec.LowerBound[model.ResourceCPU] = currentCPU
	}

	if rec.Target[model.ResourceMemory] < currentMemory {
		klog.V(4).InfoS("ScaleUpOnly: bloqueando scale-down de memory",
			"current", ctx.CurrentMemoryRequest,
			"recommended", rec.Target[model.ResourceMemory],
		)
		rec.Target[model.ResourceMemory] = currentMemory
		rec.LowerBound[model.ResourceMemory] = currentMemory
	}

	return rec
}