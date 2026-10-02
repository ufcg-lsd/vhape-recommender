package routines

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/initialrequests"
)

// ensureInitialRequestsAnnotation snapshots the target workload's declared
// requests exactly once.
func (r *recommender) ensureInitialRequestsAnnotation(
	ctx context.Context,
	observedVPA *vpaautoscalingv1.VerticalPodAutoscaler,
) error {
	if observedVPA == nil || r.targetFetcher == nil || r.vpaClient == nil {
		return nil
	}
	if _, found := observedVPA.Annotations[initialrequests.Annotation]; found {
		return nil
	}

	// Objects returned by the VPA informer cache are read-only. Update a deep
	// copy so the cache remains owned exclusively by the informer.
	current := observedVPA.DeepCopy()

	template, err := r.targetFetcher.FetchPodTemplate(ctx, current)
	if err != nil {
		return fmt.Errorf("fetch target Pod template: %w", err)
	}

	payload, err := json.Marshal(initialrequests.FromPodTemplate(template))
	if err != nil {
		return fmt.Errorf("marshal initial requests: %w", err)
	}

	if current.Annotations == nil {
		current.Annotations = make(map[string]string)
	}
	current.Annotations[initialrequests.Annotation] = string(payload)
	vpaClient := r.vpaClient.VerticalPodAutoscalers(observedVPA.Namespace)
	if _, err = vpaClient.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update VPA annotation: %w", err)
	}

	return nil
}
