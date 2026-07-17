package vpaservice

import (
	"context"
	"sort"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vhapeinformerfactory "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
	"k8s.io/client-go/tools/cache"
)

func TestNewVPAService(t *testing.T) {
	client := vpafake.NewSimpleClientset()
	factory := vhapeinformerfactory.NewSharedInformerFactory(client, 0)
	informer := factory.Autoscaling().V1().VerticalPodAutoscalers()

	if _, err := NewVPAService(informer, client); err != nil {
		t.Fatalf("NewVPAService returned error: %v", err)
	}

	if _, err := NewVPAService(nil, client); err == nil {
		t.Fatal("expected error for nil informer")
	}

	if _, err := NewVPAService(informer, nil); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestListForDeploymentReturnsOnlyVPAsTargetingDeployment(t *testing.T) {
	// The VPA cache has VPAs for multiple deployments and namespaces.
	// ListForDeployment should return only VPAs whose targetRef points to the requested Deployment.
	service, informer, _ := newTestService(t)
	dep := newDeployment(testNamespace, testDeploymentName)

	generatedVPAForTargetDeployment := managedVPAForDeployment("generated-api", dep)
	manualVPAForTargetDeployment := unmanagedVPAForDeployment("manual-api", dep)
	generatedVPAForOtherDeployment := managedVPAForDeployment("generated-worker", newDeployment(testNamespace, "worker"))
	generatedVPAForSameDeploymentNameInOtherNamespace := managedVPAForDeployment("generated-api", newDeployment("staging", testDeploymentName))

	addToVPAIndexer(t, informer, generatedVPAForTargetDeployment)
	addToVPAIndexer(t, informer, manualVPAForTargetDeployment)
	addToVPAIndexer(t, informer, generatedVPAForOtherDeployment)
	addToVPAIndexer(t, informer, generatedVPAForSameDeploymentNameInOtherNamespace)

	got, err := service.ListForDeployment(dep)
	if err != nil {
		t.Fatalf("ListForDeployment returned error: %v", err)
	}

	assertVPANames(t, got, []string{"generated-api", "manual-api"})
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
	dep := newDeployment(testNamespace, testDeploymentName)

	generatedVPA := managedVPAForDeployment("generated-api", dep)
	manualVPA := unmanagedVPAForDeployment("manual-api", dep)

	service, _, client := newTestService(t, generatedVPA, manualVPA)

	if err := service.EnsureNoGeneratedVPAForDeployment(ctx, []*vpav1.VerticalPodAutoscaler{generatedVPA, manualVPA}, "test"); err != nil {
		t.Fatalf("EnsureNoGeneratedVPAForDeployment returned error: %v", err)
	}

	assertVPANotFound(t, client, generatedVPA.Namespace, generatedVPA.Name)
	assertVPAExists(t, client, manualVPA.Namespace, manualVPA.Name)
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
	dep := newDeployment(testNamespace, testDeploymentName)
	options := newGenerationOptions()

	desiredVPA, err := GenerateVPAForDeployment(NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	createdVPA, err := service.ApplyVPA(ctx, desiredVPA)
	if err != nil {
		t.Fatalf("ApplyVPA returned error: %v", err)
	}

	assertGeneratedVPA(t, createdVPA, dep, options)

	storedVPA, err := client.AutoscalingV1().VerticalPodAutoscalers(testNamespace).Get(ctx, NameForDeployment(dep), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get applied VPA: %v", err)
	}
	assertGeneratedVPA(t, storedVPA, dep, options)
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
	service, _, _ := newTestService(t)
	dep := newDeployment(testNamespace, testDeploymentName)
	options := newGenerationOptions()

	desiredVPA, err := GenerateVPAForDeployment(NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, _ = newTestService(t, desiredVPA)

	if _, err := service.ApplyVPA(ctx, desiredVPA); err == nil {
		t.Fatal("expected error for existing VPA")
	}
}

func TestCreatesVPAWhenNoneExistsForManagedDeployment(t *testing.T) {
	// A watched Deployment with no associated VPA should receive one generated VPA.
	ctx := context.Background()
	service, _, client := newTestService(t)
	dep := newDeployment(testNamespace, testDeploymentName)
	options := newGenerationOptions()

	if err := service.EnsureOneGeneratedVPAForDeployment(ctx, dep, nil, options); err != nil {
		t.Fatalf("EnsureOneGeneratedVPAForDeployment returned error: %v", err)
	}

	createdVPA, err := client.AutoscalingV1().VerticalPodAutoscalers(testNamespace).Get(ctx, NameForDeployment(dep), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created VPA: %v", err)
	}
	assertGeneratedVPA(t, createdVPA, dep, options)
}

func TestKeepsCurrentGeneratedVPAAndDeletesExtraGeneratedVPA(t *testing.T) {
	// When the desired generated VPA already exists, it should be kept.
	// Extra generated VPAs for the same Deployment should be deleted.
	ctx := context.Background()
	service, _, _ := newTestService(t)
	dep := newDeployment(testNamespace, testDeploymentName)
	options := newGenerationOptions()

	currentGeneratedVPA, err := GenerateVPAForDeployment(NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}
	extraGeneratedVPA := managedVPAForDeployment("generated-api-extra", dep)

	service, _, client := newTestService(t, currentGeneratedVPA, extraGeneratedVPA)

	if err := service.EnsureOneGeneratedVPAForDeployment(ctx, dep, []*vpav1.VerticalPodAutoscaler{currentGeneratedVPA, extraGeneratedVPA}, options); err != nil {
		t.Fatalf("EnsureOneGeneratedVPAForDeployment returned error: %v", err)
	}

	assertVPAExists(t, client, currentGeneratedVPA.Namespace, currentGeneratedVPA.Name)
	assertVPANotFound(t, client, extraGeneratedVPA.Namespace, extraGeneratedVPA.Name)
}

func TestReplacesOutdatedGeneratedVPA(t *testing.T) {
	// A generated VPA can become outdated when the watched namespace changes policy or update mode.
	// The service should delete the old generated VPA and create the desired one.
	ctx := context.Background()
	service, _, _ := newTestService(t)
	dep := newDeployment(testNamespace, testDeploymentName)
	newOptions := newGenerationOptions()
	oldOptions := GenerationOptions{
		VhapePolicyNamespace: "old-system",
		VhapePolicyName:      "old-policy",
		VPAUpdateMode:        vpav1.UpdateModeRecreate,
	}

	outdatedGeneratedVPA, err := GenerateVPAForDeployment(NameForDeployment(dep), dep, oldOptions)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	service, _, client := newTestService(t, outdatedGeneratedVPA)

	if err := service.EnsureOneGeneratedVPAForDeployment(ctx, dep, []*vpav1.VerticalPodAutoscaler{outdatedGeneratedVPA}, newOptions); err != nil {
		t.Fatalf("EnsureOneGeneratedVPAForDeployment returned error: %v", err)
	}

	createdVPA, err := client.AutoscalingV1().VerticalPodAutoscalers(testNamespace).Get(ctx, NameForDeployment(dep), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created VPA: %v", err)
	}
	assertGeneratedVPA(t, createdVPA, dep, newOptions)
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
	dep := newDeployment(testNamespace, testDeploymentName)

	generatedVPA := managedVPAForDeployment("generated-api", dep)

	service, _, _ := newTestService(t)

	err := service.EnsureNoGeneratedVPAForDeployment(ctx, []*vpav1.VerticalPodAutoscaler{generatedVPA}, "test")
	if err != nil {
		t.Fatalf("EnsureNoGeneratedVPAForDeployment returned error: %v", err)
	}
}

func TestIsManagedByWatcher(t *testing.T) {
	if IsManagedByWatcher(nil) {
		t.Fatal("nil VPA should not be managed by watcher")
	}

	manualVPA := &vpav1.VerticalPodAutoscaler{}
	if IsManagedByWatcher(manualVPA) {
		t.Fatal("VPA without managed-by label should not be managed by watcher")
	}

	generatedVPA := managedVPAForDeployment("managed-api", newDeployment(testNamespace, testDeploymentName))
	if !IsManagedByWatcher(generatedVPA) {
		t.Fatal("VPA with managed-by label should be managed by watcher")
	}
}

func TestIsDesiredGeneratedVPA(t *testing.T) {
	dep := newDeployment(testNamespace, testDeploymentName)
	options := newGenerationOptions()
	desiredGeneratedVPA, err := GenerateVPAForDeployment(NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment returned error: %v", err)
	}

	if !isDesiredGeneratedVPA(desiredGeneratedVPA.DeepCopy(), desiredGeneratedVPA) {
		t.Fatal("identical generated VPA should be desired")
	}

	generatedVPAWithDifferentAnnotation := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentAnnotation.Annotations[vhapePolicyAnnotation] = "other/policy"
	if isDesiredGeneratedVPA(generatedVPAWithDifferentAnnotation, desiredGeneratedVPA) {
		t.Fatal("VPA with different annotations should not be desired")
	}

	generatedVPAWithDifferentSpec := desiredGeneratedVPA.DeepCopy()
	mode := vpav1.UpdateModeRecreate
	generatedVPAWithDifferentSpec.Spec.UpdatePolicy.UpdateMode = &mode
	if isDesiredGeneratedVPA(generatedVPAWithDifferentSpec, desiredGeneratedVPA) {
		t.Fatal("VPA with different spec should not be desired")
	}

	generatedVPAWithDifferentLabel := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentLabel.Labels[ManagedByLabel] = "other-manager"
	if isDesiredGeneratedVPA(generatedVPAWithDifferentLabel, desiredGeneratedVPA) {
		t.Fatal("VPA with different labels should not be desired")
	}


	generatedVPAWithDifferentUID := desiredGeneratedVPA.DeepCopy()
	generatedVPAWithDifferentUID.OwnerReferences[0].UID = "old-uid"
	if isDesiredGeneratedVPA(generatedVPAWithDifferentUID, desiredGeneratedVPA) {
		t.Fatal("VPA with different ownerReference UID should not be desired")
	}

	if isDesiredGeneratedVPA(nil, desiredGeneratedVPA) {
		t.Fatal("nil current VPA should not be desired")
	}
	if isDesiredGeneratedVPA(desiredGeneratedVPA, nil) {
		t.Fatal("nil desired VPA should not be desired")
	}
}

func newTestService(t *testing.T, objects ...*vpav1.VerticalPodAutoscaler) (*VPAService, cache.SharedIndexInformer, *vpafake.Clientset) {
	t.Helper()

	client := vpafake.NewSimpleClientset(toRuntimeObjects(objects...)...)
	factory := vhapeinformerfactory.NewSharedInformerFactory(client, 0)
	informer := factory.Autoscaling().V1().VerticalPodAutoscalers()

	if err := AddDeploymentToVPAsIndex(informer); err != nil {
		t.Fatalf("AddDeploymentToVPAsIndex returned error: %v", err)
	}

	service, err := NewVPAService(informer, client)
	if err != nil {
		t.Fatalf("NewVPAService returned error: %v", err)
	}

	return service, informer.Informer(), client
}

func toRuntimeObjects(vpas ...*vpav1.VerticalPodAutoscaler) []runtime.Object {
	objects := make([]runtime.Object, 0, len(vpas))
	for _, vpa := range vpas {
		if vpa != nil {
			objects = append(objects, vpa.DeepCopy())
		}
	}
	return objects
}

func addToVPAIndexer(t *testing.T, informer cache.SharedIndexInformer, vpa *vpav1.VerticalPodAutoscaler) {
	t.Helper()
	if err := informer.GetIndexer().Add(vpa); err != nil {
		t.Fatalf("add VPA to indexer: %v", err)
	}
}

func assertVPAExists(t *testing.T, client *vpafake.Clientset, namespace, name string) {
	t.Helper()
	if _, err := client.AutoscalingV1().VerticalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected VPA %s/%s to exist: %v", namespace, name, err)
	}
}

func assertVPANotFound(t *testing.T, client *vpafake.Clientset, namespace, name string) {
	t.Helper()
	_, err := client.AutoscalingV1().VerticalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected VPA %s/%s to be not found, got %v", namespace, name, err)
	}
}

func assertVPANames(t *testing.T, vpas []*vpav1.VerticalPodAutoscaler, want []string) {
	t.Helper()

	got := make([]string, 0, len(vpas))
	for _, vpa := range vpas {
		got = append(got, vpa.Name)
	}

	sort.Strings(got)
	sort.Strings(want)

	gotJoined := strings.Join(got, ",")
	wantJoined := strings.Join(want, ",")
	if gotJoined != wantJoined {
		t.Fatalf("VPA names = [%s], want [%s]", gotJoined, wantJoined)
	}
}
