package vpaservice

import (
	"reflect"
	"testing"
)

func TestAddDeploymentToVPAsIndexRejectsNilInformer(t *testing.T) {
	if err := AddDeploymentToVPAsIndex(nil); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAssociatedVPADeploymentKey(t *testing.T) {
	dep := newDeployment(testNamespace, testDeploymentName)

	vpa, err := GenerateVPAForDeployment(testVpaName, dep, newGenerationOptions())
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	keys, err := getAssociatedVPADeploymentKey(vpa)
	if err != nil {
		t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
	}

	assertKeys(t, keys, namespacedKey(testNamespace, testDeploymentName))
}

func TestAssociatedVPADeploymentKeyIgnoresNonVPAObject(t *testing.T) {
	keys, err := getAssociatedVPADeploymentKey("not-a-vpa")
	if err != nil {
		t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
	}

	assertKeys(t, keys)
}

func TestAssociatedVPADeploymentKeyIgnoresNilTargetRef(t *testing.T) {
	dep := newDeployment(testNamespace, testDeploymentName)
	vpa, err := GenerateVPAForDeployment(testVpaName, dep, newGenerationOptions())
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}
	vpa.Spec.TargetRef = nil

	keys, err := getAssociatedVPADeploymentKey(vpa)
	if err != nil {
		t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
	}

	assertKeys(t, keys)
}

func TestAssociatedVPADeploymentKeyIgnoresInvalidTargets(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		kind       string
		targetName string
	}{
		{
			name:       "wrong apiVersion",
			apiVersion: "apps/v2",
			kind:       deploymentKind,
			targetName: testDeploymentName,
		},
		{
			name:       "empty apiVersion",
			apiVersion: "",
			kind:       deploymentKind,
			targetName: testDeploymentName,
		},
		{
			name:       "wrong kind",
			apiVersion: deploymentAPIVersion,
			kind:       "StatefulSet",
			targetName: testDeploymentName,
		},
		{
			name:       "empty kind",
			apiVersion: deploymentAPIVersion,
			kind:       "",
			targetName: testDeploymentName,
		},
		{
			name:       "empty target name",
			apiVersion: deploymentAPIVersion,
			kind:       deploymentKind,
			targetName: "",
		},
	}

	for _, tt := range tests {
		dep := newDeployment(testNamespace, testDeploymentName)
		t.Run(tt.name, func(t *testing.T) {
			vpa, err := GenerateVPAForDeployment(testVpaName, dep, newGenerationOptions())
			if err != nil {
				t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
			}
			
			vpa.Spec.TargetRef.APIVersion = tt.apiVersion
			vpa.Spec.TargetRef.Kind = tt.kind
			vpa.Spec.TargetRef.Name = tt.targetName

			keys, err := getAssociatedVPADeploymentKey(vpa)
			if err != nil {
				t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
			}

			assertKeys(t, keys)
		})
	}
}

func TestNamespacedKey(t *testing.T) {
	got := namespacedKey(testNamespace, testDeploymentName)
	want := testNamespace + "/" + testDeploymentName

	if got != want {
		t.Fatalf("namespacedKey() = %q, want %q", got, want)
	}
}

func TestIsDeploymentTarget(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		kind       string
		targetName string
		want       bool
	}{
		{
			name:       "deployment target",
			apiVersion: deploymentAPIVersion,
			kind:       deploymentKind,
			targetName: testDeploymentName,
			want:       true,
		},
		{
			name:       "deployment target lowercase kind",
			apiVersion: deploymentAPIVersion,
			kind:       "deployment",
			targetName: testDeploymentName,
			want:       true,
		},
		{
			name:       "wrong apiVersion",
			apiVersion: "extensions/v1beta1",
			kind:       deploymentKind,
			targetName: testDeploymentName,
			want:       false,
		},
		{
			name:       "wrong kind",
			apiVersion: deploymentAPIVersion,
			kind:       "StatefulSet",
			targetName: testDeploymentName,
			want:       false,
		},
		{
			name:       "empty target name",
			apiVersion: deploymentAPIVersion,
			kind:       deploymentKind,
			targetName: "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDeploymentTarget(tt.apiVersion, tt.kind, tt.targetName)
			if got != tt.want {
				t.Fatalf("isDeploymentTarget() = %t, want %t", got, tt.want)
			}
		})
	}
}

func assertKeys(t *testing.T, got []string, want ...string) {
	t.Helper()

	if got == nil {
		got = []string{}
	}
	if want == nil {
		want = []string{}
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("index keys = %#v, want %#v", got, want)
	}
}
