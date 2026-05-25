package scalingrules

import (
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/klog/v2"
)

// BlockScaleUp prevents recommendations from increasing the current request.
//
// If any recommendation value is higher than the current request, it is
// capped at the current request. UncappedTarget is intentionally left unchanged
// because it represents the estimator output before policy capping and is useful
// for debugging.
type BlockScaleUp struct{}

func (r *BlockScaleUp) Apply(
	rec recommendation.SingleResourceRecommendation,
	containerName string,
	resourceName model.ResourceName,
	currentRequest model.ResourceAmount,
) recommendation.SingleResourceRecommendation {
	if currentRequest <= 0 {
		return rec
	}

	adjustedRec, changed := capSingleResourceRecommendation(rec, currentRequest)
	if changed {
		klog.V(4).InfoS(
			"BlockScaleUp: capped recommendation at current request",
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


func capSingleResourceRecommendation(rec recommendation.SingleResourceRecommendation, max model.ResourceAmount) (recommendation.SingleResourceRecommendation, bool) {
	changed := false

	if rec.Target > max {
		rec.Target = max
		changed = true
	}
	if rec.LowerBound > max {
		rec.LowerBound = max
		changed = true
	}
	if rec.UpperBound > max {
		rec.UpperBound = max
		changed = true
	}

	return rec, changed
}