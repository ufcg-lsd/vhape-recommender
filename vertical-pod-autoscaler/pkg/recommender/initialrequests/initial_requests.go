// Package initialrequests manages the VPA annotation that snapshots workload
// requests before recommendations can change them.
package initialrequests

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const Annotation = "autoscaling.vhape.io/initial-requests"

// Snapshot is the workload template's requests when the recommender first observes a VPA.
type Snapshot struct {
	Containers map[string]corev1.ResourceList `json:"containers"`
}

// FromPodTemplate captures requests from every regular container.
func FromPodTemplate(template *corev1.PodTemplateSpec) Snapshot {
	snapshot := Snapshot{Containers: make(map[string]corev1.ResourceList, len(template.Spec.Containers))}
	for _, container := range template.Spec.Containers {
		snapshot.Containers[container.Name] = container.Resources.Requests.DeepCopy()
	}
	return snapshot
}

// Request returns a captured resource request for one container.
func Request(annotations map[string]string, containerName string, resourceName model.ResourceName) (model.ResourceAmount, error) {
	raw, found := annotations[Annotation]
	if !found {
		return 0, fmt.Errorf("missing %s annotation", Annotation)
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return 0, fmt.Errorf("decode %s annotation: %w", Annotation, err)
	}
	requests, found := snapshot.Containers[containerName]
	if !found {
		return 0, fmt.Errorf("missing original requests for container %q", containerName)
	}

	switch resourceName {
	case model.ResourceCPU:
		quantity, found := requests[corev1.ResourceCPU]
		if !found {
			return 0, fmt.Errorf("missing original CPU request for container %q", containerName)
		}
		return model.ResourceAmount(quantity.MilliValue()), nil
	case model.ResourceMemory:
		quantity, found := requests[corev1.ResourceMemory]
		if !found {
			return 0, fmt.Errorf("missing original memory request for container %q", containerName)
		}
		return model.ResourceAmount(quantity.Value()), nil
	default:
		return 0, fmt.Errorf("unsupported resource %q", resourceName)
	}
}
