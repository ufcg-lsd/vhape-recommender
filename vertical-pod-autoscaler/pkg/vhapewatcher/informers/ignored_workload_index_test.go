package informers

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
)

func TestAddDeploymentToIgnoredWorkloadsIndexRejectsNilInformer(t *testing.T) {
	if err := AddDeploymentToIgnoredWorkloadsIndex(nil); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetAssociatedIgnoredWorkloadDeploymentKey(t *testing.T) {
	const (
		namespace      = "test-namespace"
		deploymentName = "test-deployment"
	)

	ignoredWorkload := newIgnoredWorkloadForIndexTest(
		"test-ignored-workload",
		appsv1.SchemeGroupVersion.String(),
		"Deployment",
		namespace,
		deploymentName,
	)

	keys, err := GetAssociatedIgnoredWorkloadDeploymentKey(ignoredWorkload)
	if err != nil {
		t.Fatalf("GetAssociatedIgnoredWorkloadDeploymentKey() returned error: %v", err)
	}

	want := []string{NamespacedKey(namespace, deploymentName)}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("GetAssociatedIgnoredWorkloadDeploymentKey() = %#v, want %#v", keys, want)
	}
}

func TestGetAssociatedIgnoredWorkloadDeploymentKeyIgnoresUnsupportedObjects(t *testing.T) {
	const (
		namespace      = "test-namespace"
		deploymentName = "test-deployment"
	)

	tests := []struct {
		name string
		obj  interface{}
	}{
		{
			name: "non VhapeIgnoredWorkload object",
			obj:  "not-an-ignored-workload",
		},
		{
			name: "nil VhapeIgnoredWorkload",
			obj:  (*vhapev1alpha1.VhapeIgnoredWorkload)(nil),
		},
		{
			name: "wrong apiVersion",
			obj: newIgnoredWorkloadForIndexTest(
				"test-ignored-workload",
				"apps/v2",
				"Deployment",
				namespace,
				deploymentName,
			),
		},
		{
			name: "wrong kind",
			obj: newIgnoredWorkloadForIndexTest(
				"test-ignored-workload",
				appsv1.SchemeGroupVersion.String(),
				"StatefulSet",
				namespace,
				deploymentName,
			),
		},
		{
			name: "empty namespace",
			obj: newIgnoredWorkloadForIndexTest(
				"test-ignored-workload",
				appsv1.SchemeGroupVersion.String(),
				"Deployment",
				"",
				deploymentName,
			),
		},
		{
			name: "empty target name",
			obj: newIgnoredWorkloadForIndexTest(
				"test-ignored-workload",
				appsv1.SchemeGroupVersion.String(),
				"Deployment",
				namespace,
				"",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := GetAssociatedIgnoredWorkloadDeploymentKey(tt.obj)
			if err != nil {
				t.Fatalf("GetAssociatedIgnoredWorkloadDeploymentKey() returned error: %v", err)
			}
			if keys != nil {
				t.Fatalf("GetAssociatedIgnoredWorkloadDeploymentKey() = %#v, want nil", keys)
			}
		})
	}
}

func newIgnoredWorkloadForIndexTest(name, apiVersion, kind, namespace, targetName string) *vhapev1alpha1.VhapeIgnoredWorkload {
	return &vhapev1alpha1.VhapeIgnoredWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: vhapev1alpha1.VhapeIgnoredWorkloadSpec{
			TargetRef: corev1.ObjectReference{
				APIVersion: apiVersion,
				Kind:       kind,
				Namespace:  namespace,
				Name:       targetName,
			},
		},
	}
}
