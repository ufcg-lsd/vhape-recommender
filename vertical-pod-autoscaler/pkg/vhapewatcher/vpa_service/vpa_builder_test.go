package vpaservice_test

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
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

func TestHasVhapeRecommenderLabel(t *testing.T) {
	tests := []struct {
		name        string
		vpa         *vpav1.VerticalPodAutoscaler
		wantLabeled bool
	}{
		{
			name: "nil VPA",
		},
		{
			name: "no labels",
			vpa:  &vpav1.VerticalPodAutoscaler{},
		},
		{
			name: "unrelated label",
			vpa:  &vpav1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"example.com/recommender": "other"}}},
		},
		{
			name:        "VHAPE label with custom recommender name",
			vpa:         &vpav1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{vpaservice.VhapeLabel: "team-a-recommender"}}},
			wantLabeled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vpaservice.HasVhapeRecommenderLabel(tt.vpa); got != tt.wantLabeled {
				t.Fatalf("HasVhapeRecommenderLabel() = %v, want %v", got, tt.wantLabeled)
			}
		})
	}
}

func TestVhapePolicyName(t *testing.T) {
	if got := vpaservice.VhapePolicyName(nil); got != "" {
		t.Fatalf("VhapePolicyName(nil) = %q, want empty", got)
	}

	vpa := &vpav1.VerticalPodAutoscaler{}
	if got := vpaservice.VhapePolicyName(vpa); got != "" {
		t.Fatalf("VhapePolicyName() without annotation = %q, want empty", got)
	}

	vpa.Annotations = map[string]string{vpaservice.VhapePolicyAnnotation: "test-policy"}
	if got := vpaservice.VhapePolicyName(vpa); got != "test-policy" {
		t.Fatalf("VhapePolicyName() = %q, want test-policy", got)
	}
}

func newDesiredConfig() vhapev1alpha1.VhapeWatchedNamespaceSpec {
	return vhapev1alpha1.VhapeWatchedNamespaceSpec{
		VhapePolicyName: testutil.TestPolicyName,
		VPAUpdateMode:   testutil.TestVPAUpdateMode,
	}
}
