package initialrequests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestFromPodTemplateAndRequest(t *testing.T) {
	snapshot := FromPodTemplate(&corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}},
		}},
	}})
	payload, err := json.Marshal(snapshot)
	assert.NoError(t, err)

	annotations := map[string]string{Annotation: string(payload)}
	cpu, err := Request(annotations, "app", model.ResourceCPU)
	assert.NoError(t, err)
	assert.Equal(t, model.ResourceAmount(250), cpu)
	memory, err := Request(annotations, "app", model.ResourceMemory)
	assert.NoError(t, err)
	assert.Equal(t, model.ResourceAmount(256*1024*1024), memory)
}

func TestRequestRejectsMissingAnnotation(t *testing.T) {
	_, err := Request(nil, "app", model.ResourceCPU)
	assert.ErrorContains(t, err, Annotation)
}

func TestRequestRejectsInvalidSnapshots(t *testing.T) {
	snapshot := Snapshot{Containers: map[string]corev1.ResourceList{
		"app": {corev1.ResourceCPU: resource.MustParse("250m")},
	}}
	payload, err := json.Marshal(snapshot)
	assert.NoError(t, err)

	tests := []struct {
		name       string
		annotation string
		container  string
		resource   model.ResourceName
		wantError  string
	}{
		{
			name:       "malformed annotation",
			annotation: `{`,
			container:  "app",
			resource:   model.ResourceCPU,
			wantError:  "decode",
		},
		{
			name:       "unknown container",
			annotation: string(payload),
			container:  "sidecar",
			resource:   model.ResourceCPU,
			wantError:  `container "sidecar"`,
		},
		{
			name:       "missing resource request",
			annotation: string(payload),
			container:  "app",
			resource:   model.ResourceMemory,
			wantError:  "missing original memory request",
		},
		{
			name:       "unsupported resource",
			annotation: string(payload),
			container:  "app",
			resource:   model.ResourceName("ephemeral-storage"),
			wantError:  "unsupported resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Request(map[string]string{Annotation: tt.annotation}, tt.container, tt.resource)
			assert.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestFromPodTemplateCopiesRequests(t *testing.T) {
	template := &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "app",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("250m"),
		}},
	}}}}

	snapshot := FromPodTemplate(template)
	template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("1")

	cpu := snapshot.Containers["app"][corev1.ResourceCPU]
	assert.Equal(t, "250m", cpu.String())
}
