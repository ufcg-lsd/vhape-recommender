/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package routines

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/initialrequests"
)

// ensureInitialRequestsAnnotation snapshots the target workload's declared
// requests exactly once. It uses Update rather than an unconditional patch so
// resourceVersion protects an annotation written concurrently by another
// recommender instance.
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

	vpaClient := r.vpaClient.VerticalPodAutoscalers(observedVPA.Namespace)
	current, err := vpaClient.Get(ctx, observedVPA.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get VPA: %w", err)
	}
	if _, found := current.Annotations[initialrequests.Annotation]; found {
		return nil
	}

	template, err := r.targetFetcher.FetchPodTemplate(ctx, current)
	if err != nil {
		return fmt.Errorf("fetch target Pod template: %w", err)
	}

	payload, err := json.Marshal(initialrequests.FromPodTemplate(template))
	if err != nil {
		return fmt.Errorf("marshal initial requests: %w", err)
	}

	// Make at most two attempts: the initial write and one retry after a conflict.
	for attempt := 0; attempt < 2; attempt++ {
		if current.Annotations == nil {
			current.Annotations = make(map[string]string)
		}
		if _, found := current.Annotations[initialrequests.Annotation]; found {
			return nil
		}
		current.Annotations[initialrequests.Annotation] = string(payload)

		if _, err = vpaClient.Update(ctx, current, metav1.UpdateOptions{}); err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) || attempt == 1 {
			return fmt.Errorf("update VPA annotation: %w", err)
		}

		current, err = vpaClient.Get(ctx, observedVPA.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get VPA after update conflict: %w", err)
		}
	}

	return nil
}
