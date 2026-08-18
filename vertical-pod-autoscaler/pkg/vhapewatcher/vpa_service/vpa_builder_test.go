package vpaservice_test

import (
	"strings"
	"testing"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
)

func TestGenerateVPAForDeployment(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newDesiredConfig()

	vpa, err := vpaservice.GenerateVPAForDeployment(testutil.TestVPAName, dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	testutil.AssertGeneratedVPA(
		t,
		vpa,
		dep,
		testutil.TestVPAName,
		options,
	)
}

func TestGenerateVPAForDeploymentRejectsNilDeployment(t *testing.T) {
	vpa, err := vpaservice.GenerateVPAForDeployment(testutil.TestVPAName, nil, newDesiredConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if vpa != nil {
		t.Fatalf("expected nil VPA, got %#v", vpa)
	}
}

func TestGenerateVPAForDeploymentRejectsEmptyName(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	vpa, err := vpaservice.GenerateVPAForDeployment("", dep, newDesiredConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if vpa != nil {
		t.Fatalf("expected nil VPA, got %#v", vpa)
	}
}

func TestNameForDeployment(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	got := vpaservice.NameForDeployment(dep)
	want := vpaservice.GeneratedVPANamePrefix + testutil.TestDeploymentName

	if got != want {
		t.Fatalf("NameForDeployment() = %q, want %q", got, want)
	}
}

func TestNameForDeploymentTruncates(t *testing.T) {
	longDeploymentName := strings.Repeat("a", 300)
	dep := testutil.NewDeployment(testutil.TestNamespace, longDeploymentName)

	got := vpaservice.NameForDeployment(dep)

	if len(got) != 253 {
		t.Fatalf("len(NameForDeployment()) = %d, want 253", len(got))
	}
	if !strings.HasPrefix(got, vpaservice.GeneratedVPANamePrefix) {
		t.Fatalf("expected name to keep generated prefix, got %q", got)
	}
}

func TestLabelsForVPA(t *testing.T) {
	labels := vpaservice.LabelsForVPA()

	if labels[vpaservice.ManagedByLabel] != vpaservice.ManagedByValue {
		t.Fatalf("managed-by label = %q, want %q", labels[vpaservice.ManagedByLabel], vpaservice.ManagedByValue)
	}

	if labels[vpaservice.VhapeLabel] != vpaservice.VhapeRecommenderName {
		t.Fatalf("vhape label = %q, want %q", labels[vpaservice.VhapeLabel], vpaservice.VhapeRecommenderName)
	}
}

func TestOwnerReferencesForDeployment(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	refs := vpaservice.OwnerReferencesForDeployment(dep)
	testutil.AssertOwnerReferenceForDeployment(t, refs, dep)
}

func newDesiredConfig() vhapev1alpha1.VhapeWatchedNamespaceSpec {
	return vhapev1alpha1.VhapeWatchedNamespaceSpec{
		VhapePolicyRef: vhapev1alpha1.VhapePolicyRef{
			Name:      testutil.TestPolicyName,
		},
		VPAUpdateMode:        testutil.TestVPAUpdateMode,
	}
}
