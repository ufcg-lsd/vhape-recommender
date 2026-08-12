package informers

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

func TestAddDeploymentToVPAsIndexRejectsNilInformer(t *testing.T) {
	if err := AddDeploymentToVPAsIndex(nil); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetAssociatedVPADeploymentKey(t *testing.T) {
	const (
		namespace      = "test-namespace"
		deploymentName = "test-deployment"
	)

	vpa := newVPAForIndexTest(namespace, "test-vpa", appsv1.SchemeGroupVersion.String(), "Deployment", deploymentName)

	keys, err := GetAssociatedVPADeploymentKey(vpa)
	if err != nil {
		t.Fatalf("GetAssociatedVPADeploymentKey() returned error: %v", err)
	}

	want := []string{NamespacedKey(namespace, deploymentName)}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("GetAssociatedVPADeploymentKey() = %#v, want %#v", keys, want)
	}
}

func TestGetAssociatedVPADeploymentKeyIgnoresUnsupportedObjects(t *testing.T) {
	const (
		namespace      = "test-namespace"
		deploymentName = "test-deployment"
	)

	tests := []struct {
		name string
		obj  interface{}
	}{
		{
			name: "non VPA object",
			obj:  "not-a-vpa",
		},
		{
			name: "nil VPA",
			obj:  (*vpav1.VerticalPodAutoscaler)(nil),
		},
		{
			name: "nil targetRef",
			obj: &vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-vpa", Namespace: namespace},
			},
		},
		{
			name: "wrong apiVersion",
			obj:  newVPAForIndexTest(namespace, "test-vpa", "apps/v2", "Deployment", deploymentName),
		},
		{
			name: "wrong kind",
			obj:  newVPAForIndexTest(namespace, "test-vpa", appsv1.SchemeGroupVersion.String(), "StatefulSet", deploymentName),
		},
		{
			name: "empty target name",
			obj:  newVPAForIndexTest(namespace, "test-vpa", appsv1.SchemeGroupVersion.String(), "Deployment", ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := GetAssociatedVPADeploymentKey(tt.obj)
			if err != nil {
				t.Fatalf("GetAssociatedVPADeploymentKey() returned error: %v", err)
			}
			if keys != nil {
				t.Fatalf("GetAssociatedVPADeploymentKey() = %#v, want nil", keys)
			}
		})
	}
}

func TestNamespacedKey(t *testing.T) {
	got := NamespacedKey("test-namespace", "test-deployment")
	want := "test-namespace/test-deployment"

	if got != want {
		t.Fatalf("NamespacedKey() = %q, want %q", got, want)
	}
}

func newVPAForIndexTest(namespace, name, apiVersion, kind, targetName string) *vpav1.VerticalPodAutoscaler {
	return &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: apiVersion,
				Kind:       kind,
				Name:       targetName,
			},
		},
	}
}
