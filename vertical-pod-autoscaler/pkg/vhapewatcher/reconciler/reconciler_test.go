package reconciler

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vhapeinformerfactory "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
	watcherscope "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/scope"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
	"k8s.io/client-go/tools/cache"
)

func TestNewReconciler(t *testing.T) {
	deploymentLister := testutil.NewDeploymentLister(t)
	scopeResolver := newScopeResolver(t, nil, nil)
	vpaService, _, _ := newVPAService(t)

	t.Run("returns reconciler with valid dependencies", func(t *testing.T) {
		r, err := New(deploymentLister, scopeResolver, vpaService)
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if r == nil {
			t.Fatal("New() returned nil reconciler")
		}
	})

	t.Run("rejects nil deployment lister", func(t *testing.T) {
		if _, err := New(nil, scopeResolver, vpaService); err == nil {
			t.Fatal("expected error for nil deployment lister")
		}
	})

	t.Run("rejects nil scope resolver", func(t *testing.T) {
		if _, err := New(deploymentLister, nil, vpaService); err == nil {
			t.Fatal("expected error for nil scope resolver")
		}
	})

	t.Run("rejects nil vpa service", func(t *testing.T) {
		if _, err := New(deploymentLister, scopeResolver, nil); err == nil {
			t.Fatal("expected error for nil vpa service")
		}
	})
}

func TestEnqueueDeployment(t *testing.T) {
	r, _, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	r.EnqueueDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	assertQueuedKeys(t, r, []string{testutil.TestNamespace + "/" + testutil.TestDeploymentName})
}

func TestEnqueueDeploymentIgnoresEmptyIdentity(t *testing.T) {
	r, _, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	r.EnqueueDeployment("", testutil.TestDeploymentName)
	r.EnqueueDeployment(testutil.TestNamespace, "")

	assertQueuedKeys(t, r, nil)
}

func TestEnqueueDeploymentsInNamespace(t *testing.T) {
	api := testutil.NewDeployment(testutil.TestNamespace, "api")
	worker := testutil.NewDeployment(testutil.TestNamespace, "worker")
	staging := testutil.NewDeployment("staging", "api")

	r, _, _ := newTestReconcilerWithState(t, []*appsv1.Deployment{api, worker, staging}, nil, nil, nil)

	r.EnqueueDeploymentsInNamespace(testutil.TestNamespace)

	assertQueuedKeys(t, r, []string{"producao/api", "producao/worker"})
}

func TestEnqueueDeploymentsInNamespaceIgnoresEmptyNamespace(t *testing.T) {
	r, _, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	r.EnqueueDeploymentsInNamespace("")

	assertQueuedKeys(t, r, nil)
}

func TestProcessNextWorkItemReconcilesQueuedDeployment(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
	)

	r.EnqueueDeployment(dep.Namespace, dep.Name)

	if shouldContinue := r.processNextWorkItem(ctx); !shouldContinue {
		t.Fatal("processNextWorkItem() returned false")
	}

	createdVPA := testutil.GetVPA(t, client, dep.Namespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(
		t,
		createdVPA,
		dep,
		vpaservice.NameForDeployment(dep),
		testutil.TestPolicyNamespace,
		testutil.TestPolicyName,
		testutil.TestVPAUpdateMode,
	)
	assertQueuedKeys(t, r, nil)
}

func TestReconcileDeploymentValidation(t *testing.T) {
	r, _, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	if err := r.ReconcileDeployment(context.Background(), "", testutil.TestDeploymentName); err == nil {
		t.Fatal("expected error for empty namespace")
	}

	if err := r.ReconcileDeployment(context.Background(), testutil.TestNamespace, ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestReconcileDeploymentIgnoresMissingDeployment(t *testing.T) {
	r, _, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	if err := r.ReconcileDeployment(context.Background(), testutil.TestNamespace, testutil.TestDeploymentName); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}
}

func TestReconcileDeploymentOutsideWatchedNamespaceDeletesGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := generationOptions(testutil.TestPolicyNamespace, testutil.TestPolicyName, vpav1.UpdateModeInitial)
	generatedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment() returned error: %v", err)
	}
	manualVPA := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		nil,
		nil,
		[]*vpav1.VerticalPodAutoscaler{generatedVPA, manualVPA},
	)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}

	testutil.AssertVPANotFound(t, client, generatedVPA.Namespace, generatedVPA.Name)
	testutil.AssertVPAExists(t, client, manualVPA.Namespace, manualVPA.Name)
}

func TestReconcileDeploymentIgnoredWorkloadDeletesGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	ignored := testutil.NewIgnoredWorkload("ignore-api", dep.Namespace, dep.Name)
	options := generationOptions(testutil.TestPolicyNamespace, testutil.TestPolicyName, vpav1.UpdateModeInitial)
	generatedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment() returned error: %v", err)
	}
	manualVPA := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		[]*vhapev1alpha1.VhapeIgnoredWorkload{ignored},
		[]*vpav1.VerticalPodAutoscaler{generatedVPA, manualVPA},
	)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}

	testutil.AssertVPANotFound(t, client, generatedVPA.Namespace, generatedVPA.Name)
	testutil.AssertVPAExists(t, client, manualVPA.Namespace, manualVPA.Name)
}

func TestReconcileDeploymentCreatesGeneratedVPAForWatchedDeployment(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
	)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}

	createdVPA := testutil.GetVPA(t, client, dep.Namespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(
		t,
		createdVPA,
		dep,
		vpaservice.NameForDeployment(dep),
		testutil.TestPolicyNamespace,
		testutil.TestPolicyName,
		testutil.TestVPAUpdateMode,
	)
}

func TestReconcileDeploymentPreservesManualVPAAndDeletesGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	options := generationOptions(testutil.TestPolicyNamespace, testutil.TestPolicyName, vpav1.UpdateModeInitial)
	generatedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment() returned error: %v", err)
	}
	manualVPA := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		[]*vpav1.VerticalPodAutoscaler{generatedVPA, manualVPA},
	)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}

	testutil.AssertVPANotFound(t, client, generatedVPA.Namespace, generatedVPA.Name)
	testutil.AssertVPAExists(t, client, manualVPA.Namespace, manualVPA.Name)
}

func TestReconcileDeploymentKeepsCurrentGeneratedVPAAndDeletesExtraGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := generationOptions(testutil.TestPolicyNamespace, testutil.TestPolicyName, testutil.TestVPAUpdateMode)
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	currentGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment() returned error: %v", err)
	}
	extraGeneratedVPA, err := vpaservice.GenerateVPAForDeployment("extra-generated-api", dep, options)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment() returned error: %v", err)
	}

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		[]*vpav1.VerticalPodAutoscaler{currentGeneratedVPA, extraGeneratedVPA},
	)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}

	current := testutil.GetVPA(t, client, currentGeneratedVPA.Namespace, currentGeneratedVPA.Name)
	testutil.AssertGeneratedVPA(
		t,
		current,
		dep,
		vpaservice.NameForDeployment(dep),
		testutil.TestPolicyNamespace,
		testutil.TestPolicyName,
		testutil.TestVPAUpdateMode,
	)
	testutil.AssertVPANotFound(t, client, extraGeneratedVPA.Namespace, extraGeneratedVPA.Name)
}

func TestReconcileDeploymentReplacesOutdatedGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	oldOptions := generationOptions("old-system", "old-policy", vpav1.UpdateModeRecreate)
	newOptions := generationOptions(testutil.TestPolicyNamespace, testutil.TestPolicyName, testutil.TestVPAUpdateMode)
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	outdatedGeneratedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, oldOptions)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment() returned error: %v", err)
	}

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		[]*vpav1.VerticalPodAutoscaler{outdatedGeneratedVPA},
	)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}

	createdVPA := testutil.GetVPA(t, client, dep.Namespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(
		t,
		createdVPA,
		dep,
		vpaservice.NameForDeployment(dep),
		newOptions.VhapePolicyNamespace,
		newOptions.VhapePolicyName,
		newOptions.VPAUpdateMode,
	)
}

func TestReconcileDeploymentUsesWatchedNamespacePolicyAndUpdateMode(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespaceWithPolicy(
		dep.Namespace,
		"custom-policy-namespace",
		"custom-policy",
		vpav1.UpdateModeInitial,
	)

	r, _, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
	)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() returned error: %v", err)
	}

	createdVPA := testutil.GetVPA(t, client, dep.Namespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(
		t,
		createdVPA,
		dep,
		vpaservice.NameForDeployment(dep),
		"custom-policy-namespace",
		"custom-policy",
		vpav1.UpdateModeInitial,
	)
}

func newTestReconcilerWithState(
	t *testing.T,
	deployments []*appsv1.Deployment,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
	vpas []*vpav1.VerticalPodAutoscaler,
) (*Reconciler, cache.SharedIndexInformer, *vpafake.Clientset) {
	t.Helper()

	deploymentLister := testutil.NewDeploymentLister(t, deployments...)
	scopeResolver := newScopeResolver(t, watchedNamespaces, ignoredWorkloads)
	vpaService, vpaInformer, client := newVPAService(t, vpas...)

	r, err := New(deploymentLister, scopeResolver, vpaService)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return r, vpaInformer, client
}

func newScopeResolver(
	t *testing.T,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
) *watcherscope.Scope {
	t.Helper()

	watchedLister, ignoredLister := testutil.NewVhapeListers(t, watchedNamespaces, ignoredWorkloads)

	scopeResolver, err := watcherscope.New(watchedLister, ignoredLister)
	if err != nil {
		t.Fatalf("scope.New() returned error: %v", err)
	}

	return scopeResolver
}

func newVPAService(t *testing.T, vpas ...*vpav1.VerticalPodAutoscaler) (*vpaservice.VPAService, cache.SharedIndexInformer, *vpafake.Clientset) {
	t.Helper()

	client := testutil.NewVPAClientset(vpas...)
	factory := vhapeinformerfactory.NewSharedInformerFactory(client, 0)
	informer := factory.Autoscaling().V1().VerticalPodAutoscalers()

	if err := vpaservice.AddDeploymentToVPAsIndex(informer); err != nil {
		t.Fatalf("AddDeploymentToVPAsIndex() returned error: %v", err)
	}

	service, err := vpaservice.NewVPAService(informer, client)
	if err != nil {
		t.Fatalf("NewVPAService() returned error: %v", err)
	}

	for _, vpa := range vpas {
		testutil.AddToIndexer(t, informer.Informer().GetIndexer(), vpa)
	}

	return service, informer.Informer(), client
}

func generationOptions(policyNamespace, policyName string, updateMode vpav1.UpdateMode) vpaservice.GenerationOptions {
	return vpaservice.GenerationOptions{
		VhapePolicyNamespace: policyNamespace,
		VhapePolicyName:      policyName,
		VPAUpdateMode:        updateMode,
	}
}

func assertQueuedKeys(t *testing.T, r *Reconciler, want []string) {
	t.Helper()

	if want == nil {
		want = []string{}
	}

	if gotLen := r.queue.Len(); gotLen != len(want) {
		t.Fatalf("queue length = %d, want %d", gotLen, len(want))
	}

	got := make([]string, 0, len(want))
	for range want {
		key, shutdown := r.queue.Get()
		if shutdown {
			t.Fatal("queue unexpectedly shut down")
		}

		got = append(got, key)
		r.queue.Done(key)
		r.queue.Forget(key)
	}

	testutil.AssertStringSlicesEqualIgnoringOrder(t, got, want)
}
