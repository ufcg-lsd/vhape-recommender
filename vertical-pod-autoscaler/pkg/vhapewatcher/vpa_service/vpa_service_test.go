package vpaservice_test

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"

	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

func TestNewVPAService(t *testing.T) {
	_, client, informers := testutil.NewInformers(t, nil, nil, nil, nil)
	informer := informers.VPA

	if _, err := vpaservice.NewVPAService(informer, client); err != nil {
		t.Fatalf("NewVPAService returned error: %v", err)
	}

	if _, err := vpaservice.NewVPAService(nil, client); err == nil {
		t.Fatal("expected error for nil informer")
	}

	if _, err := vpaservice.NewVPAService(informer, nil); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestListForDeploymentReturnsOnlyVPAsTargetingDeployment(t *testing.T) {
	// The VPA cache has VPAs for multiple deployments and namespaces.
	// ListForDeployment should return only VPAs whose targetRef points to the requested Deployment.
	service, informer, _ := newTestService(t)
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	// creating VPAs
	generatedVPAForTargetDeployment, err := vpaservice.GenerateVPAForDeployment("generated-api", dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	generatedVPAForOtherDeployment, err := vpaservice.GenerateVPAForDeployment(
		"generated-other-deployment",
		testutil.NewDeployment(testutil.TestNamespace, "other-deployment"),
		options,
	)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	generatedVPAForSameDeploymentNameInOtherNamespace, err := vpaservice.GenerateVPAForDeployment(
		"generated-api",
		testutil.NewDeployment("other-namespace", testutil.TestDeploymentName),
		options,
	)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	manualVPAForTargetDeployment := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)

	// adding VPAs to indexer
	testutil.AddToIndexer(t, informer.GetIndexer(), generatedVPAForTargetDeployment)
	testutil.AddToIndexer(t, informer.GetIndexer(), manualVPAForTargetDeployment)
	testutil.AddToIndexer(t, informer.GetIndexer(), generatedVPAForOtherDeployment)
	testutil.AddToIndexer(t, informer.GetIndexer(), generatedVPAForSameDeploymentNameInOtherNamespace)

	// act
	got, err := service.ListForDeployment(dep)
	if err != nil {
		t.Fatalf("ListForDeployment returned error: %v", err)
	}

	// assert
	testutil.AssertVPANames(t, got, []string{"generated-api", "manual-api"})
}

func TestListForDeploymentRejectsNilDeployment(t *testing.T) {
	service, _, _ := newTestService(t)

	if _, err := service.ListForDeployment(nil); err == nil {
		t.Fatal("expected error for nil deployment")
	}
}

func TestDeletesManagedVPAsAndPreservesManualVPAsWhenManualIsPresent(t *testing.T) {
	// The reconciler calls this when a Deployment is outside watcher scope.
	// Generated VPAs should be removed; manually owned VPAs must remain untouched.
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	generatedVPA, err := vpaservice.GenerateVPAForDeployment("generated-api", dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}
	manualVPA := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)

	service, _, client := newTestService(t, generatedVPA, manualVPA)

	if err := service.EnsureNoGeneratedVPAForDeployment(ctx, []*vpav1.VerticalPodAutoscaler{generatedVPA, manualVPA}, "test"); err != nil {
		t.Fatalf("EnsureNoGeneratedVPAForDeployment returned error: %v", err)
	}

	testutil.AssertVPANotFound(t, client, generatedVPA.Namespace, generatedVPA.Name)
	testutil.AssertVPAExists(t, client, manualVPA.Namespace, manualVPA.Name)
}

func TestEnsureNoGeneratedVPAForDeploymentIgnoresNilVPA(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)

	if err := service.EnsureNoGeneratedVPAForDeployment(ctx, []*vpav1.VerticalPodAutoscaler{nil}, "test"); err != nil {
		t.Fatalf("EnsureNoGeneratedVPAForDeployment returned error: %v", err)
	}
}

func TestApplyVPACreatesVPA(t *testing.T) {
	ctx := context.Background()
	service, _, client := newTestService(t)
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	desiredVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	appliedVPA, err := service.ApplyVPA(ctx, desiredVPA)
	if err != nil {
		t.Fatalf("ApplyVPA returned error: %v", err)
	}

	testutil.AssertGeneratedVPA(t, appliedVPA, dep, vpaservice.NameForDeployment(dep), options.VhapePolicyNamespace, options.VhapePolicyName, options.VPAUpdateMode)

	storedVPA := testutil.GetVPA(t, client, testutil.TestNamespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(t, storedVPA, dep, vpaservice.NameForDeployment(dep), options.VhapePolicyNamespace, options.VhapePolicyName, options.VPAUpdateMode)
}

func TestApplyVPARejectsNilVPA(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)

	if _, err := service.ApplyVPA(ctx, nil); err == nil {
		t.Fatal("expected error for nil VPA")
	}
}

func TestApplyVPARejectsAlreadyExistingVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	desiredVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, _ := newTestService(t, desiredVPA)

	if _, err := service.ApplyVPA(ctx, desiredVPA); err == nil {
		t.Fatal("expected error for existing VPA")
	}
}

func TestCreatesVPAWhenNoneExistsForManagedDeployment(t *testing.T) {
	// A watched Deployment with no associated VPA should receive one generated VPA.
	ctx := context.Background()
	service, _, client := newTestService(t)
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	if err := service.EnsureOneGeneratedVPAForDeployment(ctx, dep, nil, options); err != nil {
		t.Fatalf("EnsureOneGeneratedVPAForDeployment returned error: %v", err)
	}

	storedVPA := testutil.GetVPA(t, client, testutil.TestNamespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(t, storedVPA, dep, vpaservice.NameForDeployment(dep), options.VhapePolicyNamespace, options.VhapePolicyName, options.VPAUpdateMode)
}

func TestKeepsCurrentGeneratedVPAAndDeletesExtraGeneratedVPA(t *testing.T) {
	// When the desired generated VPA already exists, it should be kept.
	// Extra generated VPAs for the same Deployment should be deleted.
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	currentGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}
	extraGeneratedVPA, err := vpaservice.GenerateVPAForDeployment("generated-api-extra", dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, client := newTestService(t, currentGeneratedVPA, extraGeneratedVPA)

	if err := service.EnsureOneGeneratedVPAForDeployment(ctx, dep, []*vpav1.VerticalPodAutoscaler{currentGeneratedVPA, extraGeneratedVPA}, options); err != nil {
		t.Fatalf("EnsureOneGeneratedVPAForDeployment returned error: %v", err)
	}

	testutil.AssertVPAExists(t, client, currentGeneratedVPA.Namespace, currentGeneratedVPA.Name)
	testutil.AssertVPANotFound(t, client, extraGeneratedVPA.Namespace, extraGeneratedVPA.Name)
}

func TestPatchesOutdatedGeneratedVPA(t *testing.T) {
	// A generated VPA can become outdated when the watched namespace changes policy or update mode.
	// The service should patch the generated VPA in place.
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	newOptions := newGenerationOptions()
	oldOptions := vpaservice.GenerationOptions{
		VhapePolicyNamespace: "old-system",
		VhapePolicyName:      "old-policy",
		VPAUpdateMode:        vpav1.UpdateModeRecreate,
	}

	outdatedGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, oldOptions)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, client := newTestService(t, outdatedGeneratedVPA)

	if err := service.EnsureOneGeneratedVPAForDeployment(ctx, dep, []*vpav1.VerticalPodAutoscaler{outdatedGeneratedVPA}, newOptions); err != nil {
		t.Fatalf("EnsureOneGeneratedVPAForDeployment returned error: %v", err)
	}

	actions := client.Actions()
	if len(actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1", len(actions))
	}
	if got := actions[0].GetVerb(); got != "patch" {
		t.Fatalf("action verb = %q, want %q", got, "patch")
	}

	patchedVPA := testutil.GetVPA(t, client, testutil.TestNamespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(t, patchedVPA, dep, vpaservice.NameForDeployment(dep), newOptions.VhapePolicyNamespace, newOptions.VhapePolicyName, newOptions.VPAUpdateMode)
}

func TestDeletesOutdatedGeneratedVPAWhenPatchFails(t *testing.T) {
	// If patching fails, the outdated VPA should be deleted so the next reconciliation can recreate it.
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	oldOptions := vpaservice.GenerationOptions{
		VhapePolicyNamespace: "old-system",
		VhapePolicyName:      "old-policy",
		VPAUpdateMode:        vpav1.UpdateModeRecreate,
	}

	outdatedGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, oldOptions)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, client := newTestService(t, outdatedGeneratedVPA)
	client.PrependReactor("patch", "verticalpodautoscalers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("patch failed")
	})

	err = service.EnsureOneGeneratedVPAForDeployment(
		ctx,
		dep,
		[]*vpav1.VerticalPodAutoscaler{outdatedGeneratedVPA},
		newGenerationOptions(),
	)
	if err == nil {
		t.Fatal("expected error when patch fails")
	}

	actions := client.Actions()
	if len(actions) != 2 {
		t.Fatalf("len(actions) = %d, want 2", len(actions))
	}
	if got := actions[0].GetVerb(); got != "patch" {
		t.Fatalf("first action verb = %q, want %q", got, "patch")
	}
	if got := actions[1].GetVerb(); got != "delete" {
		t.Fatalf("second action verb = %q, want %q", got, "delete")
	}

	testutil.AssertVPANotFound(t, client, outdatedGeneratedVPA.Namespace, outdatedGeneratedVPA.Name)
}

func TestCleansUpExtraGeneratedVPAWhenPatchFails(t *testing.T) {
	// Cleanup should still run after a failed patch attempt.
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	oldOptions := vpaservice.GenerationOptions{
		VhapePolicyNamespace: "old-system",
		VhapePolicyName:      "old-policy",
		VPAUpdateMode:        vpav1.UpdateModeRecreate,
	}

	outdatedGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, oldOptions)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}
	extraGeneratedVPA, err := vpaservice.GenerateVPAForDeployment("generated-api-extra", dep, oldOptions)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, client := newTestService(t, outdatedGeneratedVPA, extraGeneratedVPA)
	client.PrependReactor("patch", "verticalpodautoscalers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("patch failed")
	})

	err = service.EnsureOneGeneratedVPAForDeployment(
		ctx,
		dep,
		[]*vpav1.VerticalPodAutoscaler{outdatedGeneratedVPA, extraGeneratedVPA},
		newGenerationOptions(),
	)
	if err == nil {
		t.Fatal("expected error when patch fails")
	}

	testutil.AssertVPANotFound(t, client, outdatedGeneratedVPA.Namespace, outdatedGeneratedVPA.Name)
	testutil.AssertVPANotFound(t, client, extraGeneratedVPA.Namespace, extraGeneratedVPA.Name)
}

func TestEnsureOneGeneratedVPAForDeploymentPreservesManualVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	currentGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}
	manualVPA := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)

	service, _, client := newTestService(t, currentGeneratedVPA, manualVPA)

	if err := service.EnsureOneGeneratedVPAForDeployment(
		ctx,
		dep,
		[]*vpav1.VerticalPodAutoscaler{currentGeneratedVPA, manualVPA},
		options,
	); err != nil {
		t.Fatalf("EnsureOneGeneratedVPAForDeployment returned error: %v", err)
	}

	testutil.AssertVPAExists(t, client, currentGeneratedVPA.Namespace, currentGeneratedVPA.Name)
	testutil.AssertVPAExists(t, client, manualVPA.Namespace, manualVPA.Name)
}

func TestPatchVPAPreservesAdditionalMetadata(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	desired, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	current := desired.DeepCopy()
	oldMode := vpav1.UpdateModeRecreate
	current.Spec.UpdatePolicy.UpdateMode = &oldMode
	current.Labels[vpaservice.VhapeLabel] = "old-recommender"
	current.Annotations[vpaservice.VhapePolicyAnnotation] = "old-system/old-policy"
	current.OwnerReferences[0].UID = "old-uid"

	current.Labels["example.com/custom"] = "label-value"
	current.Annotations["example.com/custom"] = "annotation-value"
	current.OwnerReferences = append(current.OwnerReferences, metav1.OwnerReference{
		APIVersion: "example.com/v1",
		Kind:       "Example",
		Name:       "extra-owner",
		UID:        "extra-owner-uid",
	})

	service, _, client := newTestService(t, current)

	patched, err := service.PatchVPA(ctx, current, desired)
	if err != nil {
		t.Fatalf("PatchVPA returned error: %v", err)
	}

	if got := patched.Labels["example.com/custom"]; got != "label-value" {
		t.Fatalf("custom label = %q, want %q", got, "label-value")
	}
	if got := patched.Annotations["example.com/custom"]; got != "annotation-value" {
		t.Fatalf("custom annotation = %q, want %q", got, "annotation-value")
	}
	if got := patched.Labels[vpaservice.VhapeLabel]; got != desired.Labels[vpaservice.VhapeLabel] {
		t.Fatalf("VHAPE label = %q, want %q", got, desired.Labels[vpaservice.VhapeLabel])
	}
	if got := patched.Annotations[vpaservice.VhapePolicyAnnotation]; got != desired.Annotations[vpaservice.VhapePolicyAnnotation] {
		t.Fatalf("VHAPE policy annotation = %q, want %q", got, desired.Annotations[vpaservice.VhapePolicyAnnotation])
	}
	if !vpaservice.IsDesiredGeneratedVPA(patched, desired) {
		t.Fatal("patched VPA should match watcher-owned desired state")
	}

	foundExtraOwner := false
	for _, ref := range patched.OwnerReferences {
		if ref.UID == "extra-owner-uid" {
			foundExtraOwner = true
			break
		}
	}
	if !foundExtraOwner {
		t.Fatal("additional non-controller owner reference was not preserved")
	}

	actions := client.Actions()
	if len(actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1", len(actions))
	}
	if got := actions[0].GetVerb(); got != "patch" {
		t.Fatalf("action verb = %q, want %q", got, "patch")
	}
}

func TestPatchVPARejectsNilVPA(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)
	vpa := testutil.NewVPA("api", testutil.TestNamespace, testutil.TestDeploymentName)

	if _, err := service.PatchVPA(ctx, nil, vpa); err == nil {
		t.Fatal("expected error for nil current VPA")
	}
	if _, err := service.PatchVPA(ctx, vpa, nil); err == nil {
		t.Fatal("expected error for nil desired VPA")
	}
}

func TestPatchVPARejectsDifferentVPAIdentity(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)
	current := testutil.NewVPA("api", testutil.TestNamespace, testutil.TestDeploymentName)

	tests := []struct {
		name    string
		desired *vpav1.VerticalPodAutoscaler
	}{
		{
			name: "different name",
			desired: func() *vpav1.VerticalPodAutoscaler {
				vpa := current.DeepCopy()
				vpa.Name = "other"
				return vpa
			}(),
		},
		{
			name: "different namespace",
			desired: func() *vpav1.VerticalPodAutoscaler {
				vpa := current.DeepCopy()
				vpa.Namespace = "other"
				return vpa
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := service.PatchVPA(ctx, current, tt.desired); err == nil {
				t.Fatal("expected error for different VPA identity")
			}
		})
	}
}

func TestEnsureOneGeneratedVPAForDeploymentRejectsNilDeployment(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)

	if err := service.EnsureOneGeneratedVPAForDeployment(ctx, nil, nil, newGenerationOptions()); err == nil {
		t.Fatal("expected error for nil deployment")
	}
}

func TestEnsureNoGeneratedVPAForDeploymentIgnoresAlreadyDeletedGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	generatedVPA, err := vpaservice.GenerateVPAForDeployment("generated-api", dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, _ := newTestService(t)

	err = service.EnsureNoGeneratedVPAForDeployment(ctx, []*vpav1.VerticalPodAutoscaler{generatedVPA}, "test")
	if err != nil {
		t.Fatalf("EnsureNoGeneratedVPAForDeployment returned error: %v", err)
	}
}

func TestIsManagedByWatcher(t *testing.T) {
	if vpaservice.IsManagedByWatcher(nil) {
		t.Fatal("nil VPA should not be managed by watcher")
	}

	manualVPA := &vpav1.VerticalPodAutoscaler{}
	if vpaservice.IsManagedByWatcher(manualVPA) {
		t.Fatal("VPA without managed-by label should not be managed by watcher")
	}

	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	generatedVPA, err := vpaservice.GenerateVPAForDeployment("managed-api", dep, newGenerationOptions())
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}
	if !vpaservice.IsManagedByWatcher(generatedVPA) {
		t.Fatal("VPA with managed-by label should be managed by watcher")
	}
}

func TestIsDesiredGeneratedVPA(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := newGenerationOptions()

	desiredGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(
		vpaservice.NameForDeployment(dep),
		dep,
		options,
	)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	if !vpaservice.IsDesiredGeneratedVPA(desiredGeneratedVPA.DeepCopy(), desiredGeneratedVPA) {
		t.Fatal("identical generated VPA should be desired")
	}

	generatedVPAWithExtraAnnotation := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithExtraAnnotation.Annotations["example.com/custom"] = "value"
	if !vpaservice.IsDesiredGeneratedVPA(generatedVPAWithExtraAnnotation, desiredGeneratedVPA) {
		t.Fatal("VPA with additional annotation should be desired")
	}

	generatedVPAWithDifferentPolicy := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentPolicy.Annotations[vpaservice.VhapePolicyAnnotation] = "other/policy"
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentPolicy, desiredGeneratedVPA) {
		t.Fatal("VPA with different VHAPE policy annotation should not be desired")
	}

	generatedVPAWithExtraLabel := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithExtraLabel.Labels["example.com/custom"] = "value"
	if !vpaservice.IsDesiredGeneratedVPA(generatedVPAWithExtraLabel, desiredGeneratedVPA) {
		t.Fatal("VPA with additional label should be desired")
	}

	generatedVPAWithoutVhapeLabel := desiredGeneratedVPA.DeepCopy()
	delete(generatedVPAWithoutVhapeLabel.Labels, vpaservice.VhapeLabel)
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithoutVhapeLabel, desiredGeneratedVPA) {
		t.Fatal("VPA without VHAPE label should not be desired")
	}

	generatedVPAWithoutValidVhapeLabel := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithoutValidVhapeLabel.Labels[vpaservice.VhapeLabel] = "other-label"
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithoutValidVhapeLabel, desiredGeneratedVPA) {
		t.Fatal("VPA with invalid VHAPE label should not be desired")
	}

	generatedVPAWithDifferentManager := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentManager.Labels[vpaservice.ManagedByLabel] = "other-manager"
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentManager, desiredGeneratedVPA) {
		t.Fatal("VPA with different managed-by label should not be desired")
	}

	generatedVPAWithDifferentSpec := desiredGeneratedVPA.DeepCopy()
	mode := vpav1.UpdateModeRecreate
	generatedVPAWithDifferentSpec.Spec.UpdatePolicy.UpdateMode = &mode
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentSpec, desiredGeneratedVPA) {
		t.Fatal("VPA with different spec should not be desired")
	}

	generatedVPAWithDifferentController := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentController.OwnerReferences[0].UID = "old-uid"
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentController, desiredGeneratedVPA) {
		t.Fatal("VPA with different controller owner reference should not be desired")
	}

	generatedVPAWithExtraOwnerReference := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithExtraOwnerReference.OwnerReferences = append(
		generatedVPAWithExtraOwnerReference.OwnerReferences,
		metav1.OwnerReference{
			APIVersion: "example.com/v1",
			Kind:       "Example",
			Name:       "extra-owner",
		},
	)
	if !vpaservice.IsDesiredGeneratedVPA(generatedVPAWithExtraOwnerReference, desiredGeneratedVPA) {
		t.Fatal("VPA with additional non-controller owner reference should be desired")
	}

	if vpaservice.IsDesiredGeneratedVPA(nil, desiredGeneratedVPA) {
		t.Fatal("nil current VPA should not be desired")
	}

	if vpaservice.IsDesiredGeneratedVPA(desiredGeneratedVPA, nil) {
		t.Fatal("nil desired VPA should not be desired")
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
			name:       "valid deployment target",
			apiVersion: appsv1.SchemeGroupVersion.String(),
			kind:       "Deployment",
			targetName: testutil.TestDeploymentName,
			want:       true,
		},
		{
			name:       "wrong apiVersion",
			apiVersion: "extensions/v1beta1",
			kind:       "Deployment",
			targetName: testutil.TestDeploymentName,
			want:       false,
		},
		{
			name:       "wrong kind",
			apiVersion: appsv1.SchemeGroupVersion.String(),
			kind:       "StatefulSet",
			targetName: testutil.TestDeploymentName,
			want:       false,
		},
		{
			name:       "empty name",
			apiVersion: appsv1.SchemeGroupVersion.String(),
			kind:       "Deployment",
			targetName: "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vpaservice.IsDeploymentTarget(tt.apiVersion, tt.kind, tt.targetName)
			if got != tt.want {
				t.Fatalf("IsDeploymentTarget(%q, %q, %q) = %v, want %v", tt.apiVersion, tt.kind, tt.targetName, got, tt.want)
			}
		})
	}
}

func newTestService(t *testing.T, vpas ...*vpav1.VerticalPodAutoscaler) (*vpaservice.VPAService, cache.SharedIndexInformer, *vpafake.Clientset) {
	t.Helper()

	_, client, informers := testutil.NewInformers(
		t,
		nil,
		nil,
		nil,
		vpas,
	)

	service, err := vpaservice.NewVPAService(informers.VPA, client)
	if err != nil {
		t.Fatalf("NewVPAService returned error: %v", err)
	}

	return service, informers.VPA.Informer(), client
}
