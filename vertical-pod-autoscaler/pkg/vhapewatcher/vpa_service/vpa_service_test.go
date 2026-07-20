package vpaservice_test

import (
	"context"
	"testing"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"

	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
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

func TestReplacesOutdatedGeneratedVPA(t *testing.T) {
	// A generated VPA can become outdated when the watched namespace changes policy or update mode.
	// The service should delete the old generated VPA and create the desired one.
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

	createdVPA := testutil.GetVPA(t, client, testutil.TestNamespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(t, createdVPA, dep, vpaservice.NameForDeployment(dep), newOptions.VhapePolicyNamespace, newOptions.VhapePolicyName, newOptions.VPAUpdateMode)
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
	desiredGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	if !vpaservice.IsDesiredGeneratedVPA(desiredGeneratedVPA.DeepCopy(), desiredGeneratedVPA) {
		t.Fatal("identical generated VPA should be desired")
	}

	generatedVPAWithDifferentAnnotation := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentAnnotation.Annotations[vpaservice.VhapePolicyAnnotation] = "other/policy"
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentAnnotation, desiredGeneratedVPA) {
		t.Fatal("VPA with different annotations should not be desired")
	}

	generatedVPAWithDifferentSpec := desiredGeneratedVPA.DeepCopy()
	mode := vpav1.UpdateModeRecreate
	generatedVPAWithDifferentSpec.Spec.UpdatePolicy.UpdateMode = &mode
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentSpec, desiredGeneratedVPA) {
		t.Fatal("VPA with different spec should not be desired")
	}

	generatedVPAWithDifferentLabel := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentLabel.Labels[vpaservice.ManagedByLabel] = "other-manager"
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentLabel, desiredGeneratedVPA) {
		t.Fatal("VPA with different labels should not be desired")
	}

	generatedVPAWithDifferentUID := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentUID.OwnerReferences[0].UID = "old-uid"
	if vpaservice.IsDesiredGeneratedVPA(generatedVPAWithDifferentUID, desiredGeneratedVPA) {
		t.Fatal("VPA with different ownerReference UID should not be desired")
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
			apiVersion: vpaservice.DeploymentAPIVersion,
			kind:       vpaservice.DeploymentKind,
			targetName: testutil.TestDeploymentName,
			want:       true,
		},
		{
			name:       "kind is case insensitive",
			apiVersion: vpaservice.DeploymentAPIVersion,
			kind:       "deployment",
			targetName: testutil.TestDeploymentName,
			want:       true,
		},
		{
			name:       "wrong apiVersion",
			apiVersion: "extensions/v1beta1",
			kind:       vpaservice.DeploymentKind,
			targetName: testutil.TestDeploymentName,
			want:       false,
		},
		{
			name:       "wrong kind",
			apiVersion: vpaservice.DeploymentAPIVersion,
			kind:       "StatefulSet",
			targetName: testutil.TestDeploymentName,
			want:       false,
		},
		{
			name:       "empty name",
			apiVersion: vpaservice.DeploymentAPIVersion,
			kind:       vpaservice.DeploymentKind,
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
