package scalingrules

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/klog/v2"
)

// BlockScaleDown prevents recommendations from decreasing the current request.
//
// If any recommendation value is lower than the current request, it is
// raised to the current request. UncappedTarget is intentionally left unchanged
// because it represents the estimator output before policy capping and is useful
// for debugging.
type BlockScaleDown struct{}

func (r *BlockScaleDown) Apply(
	rec recommendation.SingleResourceRecommendation,
	containerName string,
	resourceName model.ResourceName,
	currentRequest model.ResourceAmount,
) recommendation.SingleResourceRecommendation {
	if currentRequest <= 0 {
		return rec
	}

	adjustedRec, changed := floorSingleResourceRecommendation(rec, currentRequest)
	if changed {
		klog.V(4).InfoS(
			"BlockScaleDown: raised recommendation to current request",
			"containerName", containerName,
			"resource", resourceName,
			"currentRequest", currentRequest,
			"target", rec.Target,
			"lowerBound", rec.LowerBound,
			"upperBound", rec.UpperBound,
		)
	}

	return adjustedRec
}

func floorSingleResourceRecommendation(rec recommendation.SingleResourceRecommendation, min model.ResourceAmount) (recommendation.SingleResourceRecommendation, bool) {
	changed := false

	if rec.Target < min {
		rec.Target = min
		changed = true
	}
	if rec.LowerBound < min {
		rec.LowerBound = min
		changed = true
	}
	if rec.UpperBound < min {
		rec.UpperBound = min
		changed = true
	}

	return rec, changed
}