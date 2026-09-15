package target

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const targetTestNamespace = "default"

func TestFetchPodTemplate(t *testing.T) {
	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "example"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example:v1"}}},
	}

	tests := []struct {
		name       string
		kind       wellKnownController
		object     runtime.Object
		objectType runtime.Object
	}{
		{
			name: "Deployment",
			kind: deployment,
			object: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Namespace: targetTestNamespace, Name: "target"},
				Spec:       appsv1.DeploymentSpec{Template: template},
			},
			objectType: &appsv1.Deployment{},
		},
		{
			name: "CronJob",
			kind: cronJob,
			object: &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Namespace: targetTestNamespace, Name: "target"},
				Spec: batchv1.CronJobSpec{JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{Template: template},
				}},
			},
			objectType: &batchv1.CronJob{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informer := cache.NewSharedIndexInformer(&cache.ListWatch{}, tt.objectType, 0, cache.Indexers{})
			if err := informer.GetStore().Add(tt.object); err != nil {
				t.Fatalf("add target to informer store: %v", err)
			}

			fetcher := &vpaTargetSelectorFetcher{informersMap: map[wellKnownController]cache.SharedIndexInformer{
				tt.kind: informer,
			}}
			vpa := &vpa_types.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Namespace: targetTestNamespace},
				Spec: vpa_types.VerticalPodAutoscalerSpec{TargetRef: &autoscalingv1.CrossVersionObjectReference{
					Kind: string(tt.kind),
					Name: "target",
				}},
			}

			got, err := fetcher.FetchPodTemplate(context.Background(), vpa)
			if err != nil {
				t.Fatalf("FetchPodTemplate() error = %v", err)
			}
			if got.Spec.Containers[0].Image != "example:v1" {
				t.Errorf("template container image = %q, want %q", got.Spec.Containers[0].Image, "example:v1")
			}

			got.Spec.Containers[0].Image = "mutated"
			stored, exists, err := informer.GetStore().GetByKey(targetTestNamespace + "/target")
			if err != nil || !exists {
				t.Fatalf("get target from informer store: exists=%t, err=%v", exists, err)
			}
			switch target := stored.(type) {
			case *appsv1.Deployment:
				if target.Spec.Template.Spec.Containers[0].Image != "example:v1" {
					t.Error("FetchPodTemplate() mutated the cached Deployment")
				}
			case *batchv1.CronJob:
				if target.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image != "example:v1" {
					t.Error("FetchPodTemplate() mutated the cached CronJob")
				}
			}
		})
	}
}

func TestFetchPodTemplateRejectsUnsupportedTarget(t *testing.T) {
	fetcher := &vpaTargetSelectorFetcher{informersMap: map[wellKnownController]cache.SharedIndexInformer{}}
	vpa := &vpa_types.VerticalPodAutoscaler{
		Spec: vpa_types.VerticalPodAutoscalerSpec{TargetRef: &autoscalingv1.CrossVersionObjectReference{Kind: "CustomWorkload", Name: "target"}},
	}

	if _, err := fetcher.FetchPodTemplate(context.Background(), vpa); err == nil {
		t.Fatal("FetchPodTemplate() error = nil, want unsupported target error")
	}
}
