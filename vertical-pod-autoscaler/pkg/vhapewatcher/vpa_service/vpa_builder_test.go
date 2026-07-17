package vpaservice

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const (
	testVpaName         = "vpa-api"
	testNamespace       = "producao"
	testDeploymentName  = "api"
	testPolicyNamespace = "vhape-system"
	testPolicyName      = "policy-p93"
)

func TestGenerateVPAForDeployment(t *testing.T) {
	dep := newDeployment(testNamespace, testDeploymentName)
	options := newGenerationOptions()

	vpa, err := GenerateVPAForDeployment(testVpaName, dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	if vpa == nil {
		t.Fatal("expected VPA, got nil")
	}

	assertVPAMetadata(t, vpa, dep, testVpaName, options)
	assertVPATargetRef(t, vpa, dep)
	assertVPARecommender(t, vpa)
	assertVPAUpdateMode(t, vpa, options.VPAUpdateMode)
	assertVPAResourcePolicy(t, vpa)
}

func TestGenerateVPAForDeploymentRejectsNilDeployment(t *testing.T) {
	vpa, err := GenerateVPAForDeployment(testVpaName, nil, newGenerationOptions())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if vpa != nil {
		t.Fatalf("expected nil VPA, got %#v", vpa)
	}
}

func TestGenerateVPAForDeploymentRejectsEmptyName(t *testing.T) {
	dep := newDeployment(testNamespace, testDeploymentName)

	vpa, err := GenerateVPAForDeployment("", dep, newGenerationOptions())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if vpa != nil {
		t.Fatalf("expected nil VPA, got %#v", vpa)
	}
}

func TestNameForDeployment(t *testing.T) {
	dep := newDeployment(testNamespace, testDeploymentName)

	got := NameForDeployment(dep)
	want := generatedVPANamePrefix + testDeploymentName

	if got != want {
		t.Fatalf("NameForDeployment() = %q, want %q", got, want)
	}
}

func TestNameForDeploymentTruncates(t *testing.T) {
	longDeploymentName := strings.Repeat("a", 300)
	dep := newDeployment(testNamespace, longDeploymentName)

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
	want := testPolicyNamespace + "/" + testPolicyName

	if got != want {
		t.Fatalf("PolicyRef() = %q, want %q", got, want)
	}
}

func TestOwnerReferencesForDeployment(t *testing.T) {
	dep := newDeployment(testNamespace, testDeploymentName)

	refs := OwnerReferencesForDeployment(dep)
	if len(refs) != 1 {
		t.Fatalf("len(ownerReferences) = %d, want 1", len(refs))
	}

	ref := refs[0]
	if ref.APIVersion != deploymentAPIVersion {
		t.Fatalf("ownerReference.APIVersion = %q, want %q", ref.APIVersion, deploymentAPIVersion)
	}
	if ref.Kind != deploymentKind {
		t.Fatalf("ownerReference.Kind = %q, want %q", ref.Kind, deploymentKind)
	}
	if ref.Name != dep.Name {
		t.Fatalf("ownerReference.Name = %q, want %q", ref.Name, dep.Name)
	}
	if ref.UID != dep.UID {
		t.Fatalf("ownerReference.UID = %q, want %q", ref.UID, dep.UID)
	}
	if ref.Controller == nil {
		t.Fatal("ownerReference.Controller is nil")
	}
	if !*ref.Controller {
		t.Fatal("ownerReference.Controller = false, want true")
	}
}

func newDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: deploymentAPIVersion,
			Kind:       deploymentKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-" + namespace + "-" + name),
		},
	}
}

func managedVPAForDeployment(name string, dep *appsv1.Deployment) *vpav1.VerticalPodAutoscaler {
	vpa, _ := GenerateVPAForDeployment(name, dep, newGenerationOptions())
	return vpa
}

func unmanagedVPAForDeployment(name string, dep *appsv1.Deployment) *vpav1.VerticalPodAutoscaler {
	return &vpav1.VerticalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vpaAPIVersion,
			Kind:       vpaKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dep.Namespace,
			Name:      name,
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: deploymentAPIVersion,
				Kind:       deploymentKind,
				Name:       dep.Name,
			},
		},
	}
}

func assertVPAMetadata(t *testing.T, vpa *vpav1.VerticalPodAutoscaler, dep *appsv1.Deployment, desiredName string, options GenerationOptions) {
	t.Helper()

	if vpa.APIVersion != vpaAPIVersion {
		t.Fatalf("APIVersion = %q, want %q", vpa.APIVersion, vpaAPIVersion)
	}
	if vpa.Kind != vpaKind {
		t.Fatalf("Kind = %q, want %q", vpa.Kind, vpaKind)
	}
	if vpa.Namespace != dep.Namespace {
		t.Fatalf("Namespace = %q, want %q", vpa.Namespace, dep.Namespace)
	}
	if vpa.Name != desiredName {
		t.Fatalf("Name = %q, want %q", vpa.Name, desiredName)
	}
	if vpa.Labels[ManagedByLabel] != ManagedByValue {
		t.Fatalf("managed-by label = %q, want %q", vpa.Labels[ManagedByLabel], ManagedByValue)
	}
	if vpa.Annotations[vhapePolicyAnnotation] != PolicyRef(options) {
		t.Fatalf("policy annotation = %q, want %q", vpa.Annotations[vhapePolicyAnnotation], PolicyRef(options))
	}

	refs := vpa.OwnerReferences
	if len(refs) != 1 {
		t.Fatalf("len(ownerReferences) = %d, want 1", len(refs))
	}
	if refs[0].APIVersion != deploymentAPIVersion {
		t.Fatalf("ownerReference.APIVersion = %q, want %q", refs[0].APIVersion, deploymentAPIVersion)
	}
	if refs[0].Kind != deploymentKind {
		t.Fatalf("ownerReference.Kind = %q, want %q", refs[0].Kind, deploymentKind)
	}
	if refs[0].Name != dep.Name {
		t.Fatalf("ownerReference.Name = %q, want %q", refs[0].Name, dep.Name)
	}
	if refs[0].UID != dep.UID {
		t.Fatalf("ownerReference.UID = %q, want %q", refs[0].UID, dep.UID)
	}
	if refs[0].Controller == nil || !*refs[0].Controller {
		t.Fatalf("ownerReference.Controller = %#v, want true", refs[0].Controller)
	}
}

func assertVPATargetRef(t *testing.T, vpa *vpav1.VerticalPodAutoscaler, dep *appsv1.Deployment) {
	t.Helper()

	if vpa.Spec.TargetRef == nil {
		t.Fatal("TargetRef is nil")
	}
	if vpa.Spec.TargetRef.APIVersion != deploymentAPIVersion {
		t.Fatalf("TargetRef.APIVersion = %q, want %q", vpa.Spec.TargetRef.APIVersion, deploymentAPIVersion)
	}
	if vpa.Spec.TargetRef.Kind != deploymentKind {
		t.Fatalf("TargetRef.Kind = %q, want %q", vpa.Spec.TargetRef.Kind, deploymentKind)
	}
	if vpa.Spec.TargetRef.Name != dep.Name {
		t.Fatalf("TargetRef.Name = %q, want %q", vpa.Spec.TargetRef.Name, dep.Name)
	}
}

func assertVPARecommender(t *testing.T, vpa *vpav1.VerticalPodAutoscaler) {
	t.Helper()

	if len(vpa.Spec.Recommenders) != 1 {
		t.Fatalf("len(Recommenders) = %d, want 1", len(vpa.Spec.Recommenders))
	}
	if vpa.Spec.Recommenders[0] == nil {
		t.Fatal("Recommenders[0] is nil")
	}
	if vpa.Spec.Recommenders[0].Name != vhapeRecommenderName {
		t.Fatalf("recommender name = %q, want %q", vpa.Spec.Recommenders[0].Name, vhapeRecommenderName)
	}
}

func assertVPAUpdateMode(t *testing.T, vpa *vpav1.VerticalPodAutoscaler, want vpav1.UpdateMode) {
	t.Helper()

	if vpa.Spec.UpdatePolicy == nil {
		t.Fatal("UpdatePolicy is nil")
	}
	if vpa.Spec.UpdatePolicy.UpdateMode == nil {
		t.Fatal("UpdatePolicy.UpdateMode is nil")
	}
	if *vpa.Spec.UpdatePolicy.UpdateMode != want {
		t.Fatalf("UpdateMode = %q, want %q", *vpa.Spec.UpdatePolicy.UpdateMode, want)
	}
}

func assertVPAResourcePolicy(t *testing.T, vpa *vpav1.VerticalPodAutoscaler) {
	t.Helper()

	if vpa.Spec.ResourcePolicy == nil {
		t.Fatal("ResourcePolicy is nil")
	}
	policies := vpa.Spec.ResourcePolicy.ContainerPolicies
	if len(policies) != 1 {
		t.Fatalf("len(ContainerPolicies) = %d, want 1", len(policies))
	}

	policy := policies[0]
	if policy.ContainerName != vpav1.DefaultContainerResourcePolicy {
		t.Fatalf("ContainerName = %q, want %q", policy.ContainerName, vpav1.DefaultContainerResourcePolicy)
	}
	if policy.ControlledValues == nil {
		t.Fatal("ControlledValues is nil")
	}
	if *policy.ControlledValues != vpav1.ContainerControlledValuesRequestsOnly {
		t.Fatalf("ControlledValues = %q, want %q", *policy.ControlledValues, vpav1.ContainerControlledValuesRequestsOnly)
	}
}

func assertGeneratedVPA(t *testing.T, vpa *vpav1.VerticalPodAutoscaler, dep *appsv1.Deployment, options GenerationOptions) {
	t.Helper()
	assertVPAMetadata(t, vpa, dep, NameForDeployment(dep), options)
	assertVPATargetRef(t, vpa, dep)
	assertVPARecommender(t, vpa)
	assertVPAUpdateMode(t, vpa, options.VPAUpdateMode)
	assertVPAResourcePolicy(t, vpa)
}

func newGenerationOptions() GenerationOptions {
	return GenerationOptions{
		VhapePolicyNamespace: testPolicyNamespace,
		VhapePolicyName:      testPolicyName,
		VPAUpdateMode:        vpav1.UpdateModeInitial,
	}
}
