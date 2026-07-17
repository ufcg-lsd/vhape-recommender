package handler

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
)

const (
	testNamespace      = "producao"
	testDeploymentName = "api"
)

type fakeDeploymentSink struct {
	deployments []string
	namespaces  []string
}

func (s *fakeDeploymentSink) EnqueueDeployment(namespace, name string) {
	s.deployments = append(s.deployments, namespace+"/"+name)
}

func (s *fakeDeploymentSink) EnqueueDeploymentsInNamespace(namespace string) {
	s.namespaces = append(s.namespaces, namespace)
}

func TestNewHandler(t *testing.T) {
	t.Run("returns handler with valid sink", func(t *testing.T) {
		handler, err := New(&fakeDeploymentSink{})
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if handler == nil {
			t.Fatal("New() returned nil handler")
		}
	})

	t.Run("rejects nil sink", func(t *testing.T) {
		handler, err := New(nil)
		if err == nil {
			t.Fatal("New() expected error, got nil")
		}
		if handler != nil {
			t.Fatalf("New() returned handler = %#v, want nil", handler)
		}
	})
}

func newHandler(t *testing.T) (*Handler, *fakeDeploymentSink) {
	t.Helper()

	sink := &fakeDeploymentSink{}
	handler, err := New(sink)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return handler, sink
}

func newDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func newVPA(name, namespace, targetName string) *vpav1.VerticalPodAutoscaler {
	return &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: deploymentAPIVersion,
				Kind:       deploymentKind,
				Name:       targetName,
			},
		},
	}
}

func newWatchedNamespace(name string) *vhapev1alpha1.VhapeWatchedNamespace {
	return &vhapev1alpha1.VhapeWatchedNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newIgnoredWorkload(name, targetNamespace, targetName string) *vhapev1alpha1.VhapeIgnoredWorkload {
	return &vhapev1alpha1.VhapeIgnoredWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: vhapev1alpha1.VhapeIgnoredWorkloadSpec{
			TargetRef: vhapev1alpha1.TargetRef{
				APIVersion: deploymentAPIVersion,
				Kind:       deploymentKind,
				Namespace:  targetNamespace,
				Name:       targetName,
			},
			Reason: "test",
		},
	}
}

func assertDeployments(t *testing.T, sink *fakeDeploymentSink, want []string) {
	t.Helper()

	if want == nil {
		want = []string{}
	}
	if sink.deployments == nil {
		sink.deployments = []string{}
	}
	if !reflect.DeepEqual(sink.deployments, want) {
		t.Fatalf("enqueued deployments = %#v, want %#v", sink.deployments, want)
	}
}

func assertNamespaces(t *testing.T, sink *fakeDeploymentSink, want []string) {
	t.Helper()

	if want == nil {
		want = []string{}
	}
	if sink.namespaces == nil {
		sink.namespaces = []string{}
	}
	if !reflect.DeepEqual(sink.namespaces, want) {
		t.Fatalf("enqueued namespaces = %#v, want %#v", sink.namespaces, want)
	}
}
