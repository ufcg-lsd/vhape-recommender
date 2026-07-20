package vpaservice

import (
	"testing"

	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
)

func TestAddDeploymentToVPAsIndexRejectsNilInformer(t *testing.T) {
	if err := AddDeploymentToVPAsIndex(nil); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAssociatedVPADeploymentKey(t *testing.T) {
	vpa := testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName)

	keys, err := getAssociatedVPADeploymentKey(vpa)
	if err != nil {
		t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
	}

	testutil.AssertStringSlicesEqualIgnoringOrder(t, keys, []string{namespacedKey(testutil.TestNamespace, testutil.TestDeploymentName)})
}

func TestAssociatedVPADeploymentKeyIgnoresNonVPAObject(t *testing.T) {
	keys, err := getAssociatedVPADeploymentKey("not-a-vpa")
	if err != nil {
		t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
	}

	testutil.AssertStringSlicesEqual(t, keys, nil)
}

func TestAssociatedVPADeploymentKeyIgnoresNilTargetRef(t *testing.T) {
	vpa := testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName)
	vpa.Spec.TargetRef = nil

	keys, err := getAssociatedVPADeploymentKey(vpa)
	if err != nil {
		t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
	}

	testutil.AssertStringSlicesEqual(t, keys, nil)
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
			targetName: testutil.TestDeploymentName,
		},
		{
			name:       "empty apiVersion",
			apiVersion: "",
			kind:       deploymentKind,
			targetName: testutil.TestDeploymentName,
		},
		{
			name:       "wrong kind",
			apiVersion: deploymentAPIVersion,
			kind:       "StatefulSet",
			targetName: testutil.TestDeploymentName,
		},
		{
			name:       "empty kind",
			apiVersion: deploymentAPIVersion,
			kind:       "",
			targetName: testutil.TestDeploymentName,
		},
		{
			name:       "empty target name",
			apiVersion: deploymentAPIVersion,
			kind:       deploymentKind,
			targetName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vpa := testutil.NewVPAWithTarget(
				testutil.TestVPAName,
				testutil.TestNamespace,
				tt.apiVersion,
				tt.kind,
				tt.targetName,
			)

			keys, err := getAssociatedVPADeploymentKey(vpa)
			if err != nil {
				t.Fatalf("getAssociatedVPADeploymentKey returned error: %v", err)
			}

			testutil.AssertStringSlicesEqual(t, keys, nil)
		})
	}
}

func TestNamespacedKey(t *testing.T) {
	got := namespacedKey(testutil.TestNamespace, testutil.TestDeploymentName)
	want := testutil.TestNamespace + "/" + testutil.TestDeploymentName

	if got != want {
		t.Fatalf("namespacedKey() = %q, want %q", got, want)
	}
}
