package vpaservice

import (
	"strings"
	"testing"

	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
)

func TestGenerateVPAForDeployment(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	vpa, err := GenerateVPAForDeployment(testutil.TestVPAName, dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	testutil.AssertGeneratedVPA(
		t,
		vpa,
		dep,
		testutil.TestVPAName,
		options.VhapePolicyNamespace,
		options.VhapePolicyName,
		options.VPAUpdateMode,
	)
}

func TestGenerateVPAForDeploymentRejectsNilDeployment(t *testing.T) {
	vpa, err := GenerateVPAForDeployment(testutil.TestVPAName, nil, newGenerationOptions())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if vpa != nil {
		t.Fatalf("expected nil VPA, got %#v", vpa)
	}
}

func TestGenerateVPAForDeploymentRejectsEmptyName(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	vpa, err := GenerateVPAForDeployment("", dep, newGenerationOptions())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if vpa != nil {
		t.Fatalf("expected nil VPA, got %#v", vpa)
	}
}

func TestNameForDeployment(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	got := NameForDeployment(dep)
	want := generatedVPANamePrefix + testutil.TestDeploymentName

	if got != want {
		t.Fatalf("NameForDeployment() = %q, want %q", got, want)
	}
}

func TestNameForDeploymentTruncates(t *testing.T) {
	longDeploymentName := strings.Repeat("a", 300)
	dep := testutil.NewDeployment(testutil.TestNamespace, longDeploymentName)

	got := NameForDeployment(dep)

	if len(got) != 253 {
		t.Fatalf("len(NameForDeployment()) = %d, want 253", len(got))
	}
	if !strings.HasPrefix(got, generatedVPANamePrefix) {
		t.Fatalf("expected name to keep generated prefix, got %q", got)
	}
}

func TestPolicyRef(t *testing.T) {
	got := PolicyRef(newGenerationOptions())
	want := testutil.TestPolicyNamespace + "/" + testutil.TestPolicyName

	if got != want {
		t.Fatalf("PolicyRef() = %q, want %q", got, want)
	}
}

func TestOwnerReferencesForDeployment(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	refs := OwnerReferencesForDeployment(dep)
	testutil.AssertOwnerReferenceForDeployment(t, refs, dep)
}

func newGenerationOptions() GenerationOptions {
	return GenerationOptions{
		VhapePolicyNamespace: testutil.TestPolicyNamespace,
		VhapePolicyName:      testutil.TestPolicyName,
		VPAUpdateMode:        testutil.TestVPAUpdateMode,
	}
}
