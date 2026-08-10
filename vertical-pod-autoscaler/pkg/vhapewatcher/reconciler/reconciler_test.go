package reconciler

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
	watcherscope "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/scope"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
)

func TestNewReconciler(t *testing.T) {
	client, informers := testutil.NewInformers(t, nil, nil, nil, nil, nil, nil, nil)
	deploymentLister := informers.Deployment.Lister()
	scopeResolver := newScopeResolver(t, informers)
	vpaService := newVPAService(t, informers, client)

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
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	r.EnqueueDeployment(testutil.TestNamespace, testutil.TestDeploymentName)

	assertQueuedKeys(t, r, []string{testutil.TestNamespace + "/" + testutil.TestDeploymentName})
}

func TestEnqueueDeploymentIgnoresEmptyIdentity(t *testing.T) {
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	r.EnqueueDeployment("", testutil.TestDeploymentName)
	r.EnqueueDeployment(testutil.TestNamespace, "")

	assertQueuedKeys(t, r, nil)
}

func TestEnqueueDeploymentsInNamespace(t *testing.T) {
	api := testutil.NewDeployment(testutil.TestNamespace, "api")
	worker := testutil.NewDeployment(testutil.TestNamespace, "worker")
	staging := testutil.NewDeployment("staging", "api")

	r, _ := newTestReconcilerWithState(t, []*appsv1.Deployment{api, worker, staging}, nil, nil, nil)

	r.EnqueueDeploymentsInNamespace(testutil.TestNamespace)

	assertQueuedKeys(t, r, []string{"producao/api", "producao/worker"})
}

func TestEnqueueDeploymentsInNamespaceIgnoresEmptyNamespace(t *testing.T) {
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	r.EnqueueDeploymentsInNamespace("")

	assertQueuedKeys(t, r, nil)
}

func TestEnqueueDeploymentsMatchingNamespaceRegex(t *testing.T) {
	deployments := []*appsv1.Deployment{
		testutil.NewDeployment("prod-api", "api"),
		testutil.NewDeployment("prod-api", "worker"),
		testutil.NewDeployment("prod-jobs", "jobs"),
		testutil.NewDeployment("staging-api", "api"),
	}
	r, _ := newTestReconcilerWithState(t, deployments, nil, nil, nil)

	r.EnqueueDeploymentsMatchingNamespaceRegex(`^prod-.*$`)

	assertQueuedKeys(t, r, []string{"prod-api/api", "prod-api/worker", "prod-jobs/jobs"})
}

func TestEnqueueDeploymentsMatchingNamespaceRegexIgnoresInvalidRegex(t *testing.T) {
	r, _ := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{testutil.NewDeployment("prod-api", "api")},
		nil,
		nil,
		nil,
	)

	r.EnqueueDeploymentsMatchingNamespaceRegex(`[invalid`)

	assertQueuedKeys(t, r, nil)
}

func TestProcessNextWorkItemReconcilesQueuedDeployment(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)

	r, client := newTestReconcilerWithState(
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
		watched.Spec,
	)
	assertQueuedKeys(t, r, nil)
}

func TestProcessNextWorkItemForgetsInvalidQueueKey(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)
	defer r.queue.ShutDown()

	key := "invalid/key/with/too/many/parts"
	r.queue.Add(key)

	if shouldContinue := r.processNextWorkItem(ctx); !shouldContinue {
		t.Fatal("processNextWorkItem() returned false")
	}

	if got := r.queue.Len(); got != 0 {
		t.Fatalf("queue length = %d, want 0", got)
	}
	if got := r.queue.NumRequeues(key); got != 0 {
		t.Fatalf("NumRequeues(%q) = %d, want 0", key, got)
	}
}

func TestProcessNextWorkItemRequeuesOnReconcileError(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)
	defer r.queue.ShutDown()

	key := testutil.TestNamespace + "/"
	r.queue.Add(key)

	if shouldContinue := r.processNextWorkItem(ctx); !shouldContinue {
		t.Fatal("processNextWorkItem() returned false")
	}

	if got := r.queue.NumRequeues(key); got != 1 {
		t.Fatalf("NumRequeues(%q) = %d, want 1", key, got)
	}
}

func TestReconcileDeploymentValidation(t *testing.T) {
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

	if err := r.ReconcileDeployment(context.Background(), "", testutil.TestDeploymentName); err == nil {
		t.Fatal("expected error for empty namespace")
	}

	if err := r.ReconcileDeployment(context.Background(), testutil.TestNamespace, ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestReconcileDeploymentIgnoresMissingDeployment(t *testing.T) {
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)

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

	r, client := newTestReconcilerWithState(
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

	r, client := newTestReconcilerWithState(
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

	r, client := newTestReconcilerWithState(
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
		watched.Spec,
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

	r, client := newTestReconcilerWithState(
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

	r, client := newTestReconcilerWithState(
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
		watched.Spec,
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

	r, client := newTestReconcilerWithState(
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
		newOptions,
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

	r, client := newTestReconcilerWithState(
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
		watched.Spec,
	)
}

func newTestReconcilerWithState(
	t *testing.T,
	deployments []*appsv1.Deployment,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
	vpas []*vpav1.VerticalPodAutoscaler,
) (*Reconciler, *vpafake.Clientset) {
	t.Helper()

	client, informers := testutil.NewInformers(
		t,
		deployments,
		nil,
		watchedNamespaces,
		nil,
		nil,
		ignoredWorkloads,
		vpas,
	)

	scopeResolver := newScopeResolver(t, informers)
	vpaService := newVPAService(t, informers, client)

	r, err := New(informers.Deployment.Lister(), scopeResolver, vpaService)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return r, client
}

func newScopeResolver(t *testing.T, informers *watcherinformers.Informers) *watcherscope.Scope {
	t.Helper()

	scopeResolver, err := watcherscope.New(
		informers.VhapeWatchedNamespace.Lister(),
		informers.VhapeIgnoredWorkload.Lister(),
		informers.VhapeWatchedNamespaceRegex.Lister(),
		informers.VhapeIgnoredNamespace.Lister(),
		informers.Namespace.Lister(),
	)
	if err != nil {
		t.Fatalf("scope.New() returned error: %v", err)
	}

	return scopeResolver
}

func newVPAService(
	t *testing.T,
	informers *watcherinformers.Informers,
	client *vpafake.Clientset,
) *vpaservice.VPAService {
	t.Helper()

	service, err := vpaservice.NewVPAService(informers.VPA, client)
	if err != nil {
		t.Fatalf("NewVPAService() returned error: %v", err)
	}

	return service
}

func generationOptions(policyNamespace, policyName string, updateMode vpav1.UpdateMode) vhapev1alpha1.VhapeWatchedNamespaceSpec {
	return vhapev1alpha1.VhapeWatchedNamespaceSpec{
		VhapePolicyRef: vhapev1alpha1.VhapePolicyRef{
			Namespace: policyNamespace,
			Name:      policyName,
		},
		VPAUpdateMode: updateMode,
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
