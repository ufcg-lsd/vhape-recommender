package testutil

import (
	"context"
	"reflect"
	"sort"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
)

func AssertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()

	if got == nil {
		got = []string{}
	}
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func AssertStringSlicesEqualIgnoringOrder(t *testing.T, got, want []string) {
	t.Helper()

	if got == nil {
		got = []string{}
	}
	if want == nil {
		want = []string{}
	}

	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)

	if !reflect.DeepEqual(gotCopy, wantCopy) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func AssertWatchedNamespaceSpec(
	t *testing.T,
	watched *vhapev1alpha1.VhapeWatchedNamespace,
	policyNamespace string,
	policyName string,
	updateMode vpav1.UpdateMode,
) {
	t.Helper()

	if watched == nil {
		t.Fatal("watched namespace is nil")
	}

	if watched.Spec.VhapePolicyRef.Namespace != policyNamespace {
		t.Fatalf("policy namespace = %q, want %q", watched.Spec.VhapePolicyRef.Namespace, policyNamespace)
	}
	if watched.Spec.VhapePolicyRef.Name != policyName {
		t.Fatalf("policy name = %q, want %q", watched.Spec.VhapePolicyRef.Name, policyName)
	}
	if watched.Spec.VPAUpdateMode != updateMode {
		t.Fatalf("VPA update mode = %q, want %q", watched.Spec.VPAUpdateMode, updateMode)
	}
}

func GetVPA(t *testing.T, client *vpafake.Clientset, namespace, name string) *vpav1.VerticalPodAutoscaler {
	t.Helper()

	vpa, err := client.AutoscalingV1().VerticalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get VPA %s/%s: %v", namespace, name, err)
	}
	return vpa
}

func AssertVPAExists(t *testing.T, client *vpafake.Clientset, namespace, name string) {
	t.Helper()

	if _, err := client.AutoscalingV1().VerticalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected VPA %s/%s to exist: %v", namespace, name, err)
	}
}

func AssertVPANotFound(t *testing.T, client *vpafake.Clientset, namespace, name string) {
	t.Helper()

	_, err := client.AutoscalingV1().VerticalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected VPA %s/%s to be not found, got %v", namespace, name, err)
	}
}

func AssertVPANames(t *testing.T, vpas []*vpav1.VerticalPodAutoscaler, want []string) {
	t.Helper()

	got := make([]string, 0, len(vpas))
	for _, vpa := range vpas {
		if vpa != nil {
			got = append(got, vpa.Name)
		}
	}

	AssertStringSlicesEqualIgnoringOrder(t, got, want)
}

func AssertVPAMetadata(
	t *testing.T,
	vpa *vpav1.VerticalPodAutoscaler,
	dep *appsv1.Deployment,
	expectedName string,
	policyName string,
) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}
	if vpa.APIVersion != vpav1.SchemeGroupVersion.String() {
		t.Fatalf("APIVersion = %q, want %q", vpa.APIVersion, vpav1.SchemeGroupVersion.String())
	}
	if vpa.Kind != "VerticalPodAutoscaler" {
		t.Fatalf("Kind = %q, want %q", vpa.Kind, "VerticalPodAutoscaler")
	}
	if vpa.Namespace != dep.Namespace {
		t.Fatalf("Namespace = %q, want %q", vpa.Namespace, dep.Namespace)
	}
	if vpa.Name != expectedName {
		t.Fatalf("Name = %q, want %q", vpa.Name, expectedName)
	}

	AssertManagedByWatcherLabel(t, vpa)
	AssertVhapeLabel(t, vpa)

	if got := vpa.Annotations[vpaservice.VhapePolicyAnnotation]; got != policyName {
		t.Fatalf("policy annotation = %q, want %q", got, policyName)
	}

	AssertVPAOwnerReference(t, vpa, dep)
}

func AssertManagedByWatcherLabel(t *testing.T, vpa *vpav1.VerticalPodAutoscaler) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}
	if vpa.Labels[vpaservice.ManagedByLabel] != vpaservice.ManagedByValue {
		t.Fatalf("managed-by label = %q, want %q", vpa.Labels[vpaservice.ManagedByLabel], vpaservice.ManagedByValue)
	}
}

func AssertVhapeLabel(t *testing.T, vpa *vpav1.VerticalPodAutoscaler) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}

	if _, ok := vpa.Labels[vpaservice.VhapeLabel]; !ok {
		t.Fatalf("expected label %q to be present", vpaservice.VhapeLabel)
	}
}

func AssertVPATargetRef(t *testing.T, vpa *vpav1.VerticalPodAutoscaler, dep *appsv1.Deployment) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}
	if vpa.Spec.TargetRef == nil {
		t.Fatal("VPA targetRef is nil")
	}
	if vpa.Spec.TargetRef.APIVersion != appsv1.SchemeGroupVersion.String() {
		t.Fatalf("targetRef apiVersion = %q, want %q", vpa.Spec.TargetRef.APIVersion, appsv1.SchemeGroupVersion.String())
	}
	if vpa.Spec.TargetRef.Kind != "Deployment" {
		t.Fatalf("targetRef kind = %q, want %q", vpa.Spec.TargetRef.Kind, "Deployment")
	}
	if vpa.Spec.TargetRef.Name != dep.Name {
		t.Fatalf("targetRef name = %q, want %q", vpa.Spec.TargetRef.Name, dep.Name)
	}
}

func AssertVPARecommender(t *testing.T, vpa *vpav1.VerticalPodAutoscaler) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}
	if len(vpa.Spec.Recommenders) != 1 {
		t.Fatalf("len(Recommenders) = %d, want 1", len(vpa.Spec.Recommenders))
	}
	if vpa.Spec.Recommenders[0] == nil {
		t.Fatal("Recommenders[0] is nil")
	}
	if vpa.Spec.Recommenders[0].Name != vpaservice.VhapeRecommenderName {
		t.Fatalf("recommender name = %q, want %q", vpa.Spec.Recommenders[0].Name, vpaservice.VhapeRecommenderName)
	}
}

func AssertVPAUpdateMode(t *testing.T, vpa *vpav1.VerticalPodAutoscaler, want vpav1.UpdateMode) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}
	if vpa.Spec.UpdatePolicy == nil {
		t.Fatal("UpdatePolicy is nil")
	}
	if vpa.Spec.UpdatePolicy.UpdateMode == nil {
		t.Fatal("UpdatePolicy.UpdateMode is nil")
	}
	if *vpa.Spec.UpdatePolicy.UpdateMode != want {
		t.Fatalf("update mode = %q, want %q", *vpa.Spec.UpdatePolicy.UpdateMode, want)
	}
}

func AssertVPAResourcePolicy(t *testing.T, vpa *vpav1.VerticalPodAutoscaler) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}
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

func AssertOwnerReferenceForDeployment(t *testing.T, refs []metav1.OwnerReference, dep *appsv1.Deployment) {
	t.Helper()

	if len(refs) != 1 {
		t.Fatalf("len(ownerReferences) = %d, want 1", len(refs))
	}

	owner := refs[0]
	if owner.APIVersion != appsv1.SchemeGroupVersion.String() {
		t.Fatalf("ownerReference apiVersion = %q, want %q", owner.APIVersion, appsv1.SchemeGroupVersion.String())
	}
	if owner.Kind != "Deployment" {
		t.Fatalf("ownerReference kind = %q, want %q", owner.Kind, "Deployment")
	}
	if owner.Name != dep.Name {
		t.Fatalf("ownerReference name = %q, want %q", owner.Name, dep.Name)
	}
	if owner.UID != dep.UID {
		t.Fatalf("ownerReference UID = %q, want %q", owner.UID, dep.UID)
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Fatalf("ownerReference controller = %#v, want true", owner.Controller)
	}
}

func AssertVPAOwnerReference(t *testing.T, vpa *vpav1.VerticalPodAutoscaler, dep *appsv1.Deployment) {
	t.Helper()

	if vpa == nil {
		t.Fatal("VPA is nil")
	}
	AssertOwnerReferenceForDeployment(t, vpa.OwnerReferences, dep)
}

func AssertGeneratedVPA(
	t *testing.T,
	vpa *vpav1.VerticalPodAutoscaler,
	dep *appsv1.Deployment,
	expectedName string,
	policyName string,
	updateMode vpav1.UpdateMode,
) {
	t.Helper()

	AssertVPAMetadata(t, vpa, dep, expectedName, policyName)
	AssertVPATargetRef(t, vpa, dep)
	AssertVPARecommender(t, vpa)
	AssertVPAUpdateMode(t, vpa, updateMode)
	AssertVPAResourcePolicy(t, vpa)
}
