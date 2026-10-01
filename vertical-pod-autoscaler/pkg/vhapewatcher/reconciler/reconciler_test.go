package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	hpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/hpa_service"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
	watcherscope "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/scope"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNewReconciler(t *testing.T) {
	client, informers := testutil.NewInformers(t, nil, nil, nil, nil, nil, nil, nil, nil)
	deploymentLister := informers.Deployment.Lister()
	scopeResolver := newScopeResolver(t, informers)
	vpaService := newVPAService(t, informers, client)
	hpaService := newHPAService(t, informers)

	t.Run("returns reconciler with valid dependencies", func(t *testing.T) {
		r, err := New(deploymentLister, scopeResolver, hpaService, vpaService)
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if r == nil {
			t.Fatal("New() returned nil reconciler")
		}
	})

	t.Run("rejects nil deployment lister", func(t *testing.T) {
		if _, err := New(nil, scopeResolver, hpaService, vpaService); err == nil {
			t.Fatal("expected error for nil deployment lister")
		}
	})

	t.Run("rejects nil scope resolver", func(t *testing.T) {
		if _, err := New(deploymentLister, nil, hpaService, vpaService); err == nil {
			t.Fatal("expected error for nil scope resolver")
		}
	})

	t.Run("rejects nil vpa service", func(t *testing.T) {
		if _, err := New(deploymentLister, scopeResolver, hpaService, nil); err == nil {
			t.Fatal("expected error for nil vpa service")
		}
	})

	t.Run("rejects nil hpa service", func(t *testing.T) {
		if _, err := New(deploymentLister, scopeResolver, nil, vpaService); err == nil {
			t.Fatal("expected error for nil hpa service")
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

func TestRunShutsDownQueueWhenContextIsCanceled(t *testing.T) {
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r.Run(ctx, 0)

	if !r.queue.ShuttingDown() {
		t.Fatal("queue is not shutting down after canceled context")
	}
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

	if shouldContinue := r.ProcessNextWorkItem(ctx); !shouldContinue {
		t.Fatal("ProcessNextWorkItem() returned false")
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

	if shouldContinue := r.ProcessNextWorkItem(ctx); !shouldContinue {
		t.Fatal("ProcessNextWorkItem() returned false")
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

	if shouldContinue := r.ProcessNextWorkItem(ctx); !shouldContinue {
		t.Fatal("ProcessNextWorkItem() returned false")
	}

	if got := r.queue.NumRequeues(key); got != 1 {
		t.Fatalf("NumRequeues(%q) = %d, want 1", key, got)
	}
}

func TestProcessNextWorkItemStopsWhenQueueIsShutDown(t *testing.T) {
	r, _ := newTestReconcilerWithState(t, nil, nil, nil, nil)
	r.queue.ShutDown()

	if shouldContinue := r.ProcessNextWorkItem(context.Background()); shouldContinue {
		t.Fatal("ProcessNextWorkItem() returned true after queue shutdown")
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

func TestReconcileDeploymentReturnsVPAEnsureError(t *testing.T) {
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	r, client := newTestReconcilerWithState(
		t,
		[]*appsv1.Deployment{dep},
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
	)
	client.PrependReactor("create", "verticalpodautoscalers", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("create failed")
	})

	if err := r.ReconcileDeployment(context.Background(), dep.Namespace, dep.Name); err == nil {
		t.Fatal("ReconcileDeployment() error = nil, want VPA ensure error")
	}
}

func TestReconcileDeploymentEnsuresVPAWhenHPAManagementFails(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	dep.Spec.Template.Spec.Containers = []corev1.Container{{
		Name: "api",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		}},
	}}
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	policy := &vhapev1alpha1.VhapePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: watched.Spec.VhapePolicyName},
		Spec:       vhapev1alpha1.VhapePolicySpec{ManageHPA: true},
	}
	generatedVPA, err := vpaservice.GenerateVPAForDeployment(vpaservice.NameForDeployment(dep), dep, watched.Spec)
	if err != nil {
		t.Fatalf("GenerateVPAForDeployment() error = %v", err)
	}
	utilization := int32(50)
	hpa := testutil.NewHPA("api-hpa", dep.Namespace, dep.Name)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &utilization,
			},
		},
	}}

	vpaClient, informers := testutil.NewInformers(
		t,
		[]*appsv1.Deployment{dep},
		nil,
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
		nil,
		[]*vpav1.VerticalPodAutoscaler{generatedVPA},
		[]*vhapev1alpha1.VhapePolicy{policy},
	)
	testutil.AddToIndexer(t, informers.HPA.Informer().GetIndexer(), hpa)
	kubeClient := kubefake.NewSimpleClientset(hpa.DeepCopy())
	wantErr := errors.New("update HPA failed")
	kubeClient.PrependReactor("update", "horizontalpodautoscalers", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, wantErr
	})
	hpaService, err := hpaservice.NewHPAService(informers.HPA, kubeClient)
	if err != nil {
		t.Fatalf("NewHPAService() error = %v", err)
	}
	r, err := New(informers.Deployment.Lister(), newScopeResolver(t, informers), hpaService, newVPAService(t, informers, vpaClient))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = r.ReconcileDeployment(ctx, dep.Namespace, dep.Name)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ReconcileDeployment() error = %v, want wrapped %v", err, wantErr)
	}

	createdVPA := testutil.GetVPA(t, vpaClient, dep.Namespace, vpaservice.NameForDeployment(dep))
	testutil.AssertGeneratedVPA(t, createdVPA, dep, vpaservice.NameForDeployment(dep), watched.Spec)
}

func TestReconcileDeploymentOutsideWatchedNamespaceDeletesGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	options := generationOptions(testutil.TestPolicyName, vpav1.UpdateModeInitial)
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
	options := generationOptions(testutil.TestPolicyName, vpav1.UpdateModeInitial)
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

func TestReconcileDeploymentCreatesVPABeforeReferencedPolicyExists(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	vpaClient, informers := testutil.NewInformers(
		t,
		[]*appsv1.Deployment{dep},
		nil,
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err := informers.VhapePolicy.Informer().GetIndexer().Delete(&vhapev1alpha1.VhapePolicy{ObjectMeta: metav1.ObjectMeta{Name: watched.Spec.VhapePolicyName}}); err != nil {
		t.Fatalf("delete VhapePolicy from indexer: %v", err)
	}
	r, err := New(
		informers.Deployment.Lister(),
		newScopeResolver(t, informers),
		newHPAService(t, informers),
		newVPAService(t, informers, vpaClient),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() error = %v", err)
	}

	created := testutil.GetVPA(t, vpaClient, dep.Namespace, vpaservice.NameForDeployment(dep))
	if got := created.Annotations[vpaservice.VhapePolicyAnnotation]; got != watched.Spec.VhapePolicyName {
		t.Fatalf("VPA policy annotation = %q, want %q", got, watched.Spec.VhapePolicyName)
	}
}

func TestReconcileDeploymentWaitsForGeneratedVPABeforeManagingHPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	dep.Spec.Template.Spec.Containers = []corev1.Container{{
		Name: "api",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		}},
	}}
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	policy := &vhapev1alpha1.VhapePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: watched.Spec.VhapePolicyName},
		Spec:       vhapev1alpha1.VhapePolicySpec{ManageHPA: true},
	}
	utilization := int32(50)
	hpa := testutil.NewHPA("api-hpa", dep.Namespace, dep.Name)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &utilization,
			},
		},
	}}
	vpaClient, informers := testutil.NewInformers(
		t,
		[]*appsv1.Deployment{dep},
		nil,
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
		nil,
		nil,
		[]*vhapev1alpha1.VhapePolicy{policy},
	)
	testutil.AddToIndexer(t, informers.HPA.Informer().GetIndexer(), hpa)
	kubeClient := kubefake.NewSimpleClientset(hpa.DeepCopy())
	hpaService, err := hpaservice.NewHPAService(informers.HPA, kubeClient)
	if err != nil {
		t.Fatalf("NewHPAService() error = %v", err)
	}
	r, err := New(
		informers.Deployment.Lister(),
		newScopeResolver(t, informers),
		hpaService,
		newVPAService(t, informers, vpaClient),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("first ReconcileDeployment() error = %v", err)
	}
	unchanged, err := kubeClient.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get HPA after first reconcile: %v", err)
	}
	if got := unchanged.Spec.Metrics[0].Resource.Target.Type; got != autoscalingv2.UtilizationMetricType {
		t.Fatalf("HPA target type after first reconcile = %q, want %q", got, autoscalingv2.UtilizationMetricType)
	}

	createdVPA := testutil.GetVPA(t, vpaClient, dep.Namespace, vpaservice.NameForDeployment(dep))
	testutil.AddToIndexer(t, informers.VPA.Informer().GetIndexer(), createdVPA)

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("second ReconcileDeployment() error = %v", err)
	}
	updated, err := kubeClient.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get HPA after second reconcile: %v", err)
	}
	target := updated.Spec.Metrics[0].Resource.Target
	if target.Type != autoscalingv2.AverageValueMetricType || target.AverageValue == nil || target.AverageValue.MilliValue() != 100 {
		t.Fatalf("HPA target after second reconcile = %#v, want AverageValue 100m", target)
	}
}

func TestReconcileDeploymentConvertsHPAWhenPolicyEnablesManagementWithManualVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	dep.Spec.Template.Spec.Containers = []corev1.Container{{
		Name: "api",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		}},
	}}
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	policy := &vhapev1alpha1.VhapePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: watched.Spec.VhapePolicyName},
		Spec:       vhapev1alpha1.VhapePolicySpec{ManageHPA: true},
	}
	utilization := int32(50)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api-hpa", Namespace: dep.Namespace},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
				Name:       dep.Name,
			},
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: &utilization,
					},
				},
			}},
		},
	}
	manualVPA := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)
	manualVPA.Annotations = map[string]string{vpaservice.VhapePolicyAnnotation: watched.Spec.VhapePolicyName}
	manualVPA.Labels = map[string]string{vpaservice.VhapeLabel: "custom-recommender"}

	vpaClient, informers := testutil.NewInformers(
		t,
		[]*appsv1.Deployment{dep},
		nil,
		[]*vhapev1alpha1.VhapeWatchedNamespace{watched},
		nil,
		nil,
		nil,
		[]*vpav1.VerticalPodAutoscaler{manualVPA},
		[]*vhapev1alpha1.VhapePolicy{policy},
	)
	testutil.AddToIndexer(t, informers.HPA.Informer().GetIndexer(), hpa)
	kubeClient := kubefake.NewSimpleClientset(hpa.DeepCopy())
	hpaService, err := hpaservice.NewHPAService(informers.HPA, kubeClient)
	if err != nil {
		t.Fatalf("NewHPAService() error = %v", err)
	}
	r, err := New(
		informers.Deployment.Lister(),
		newScopeResolver(t, informers),
		hpaService,
		newVPAService(t, informers, vpaClient),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() error = %v", err)
	}

	updated, err := kubeClient.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated HPA: %v", err)
	}
	target := updated.Spec.Metrics[0].Resource.Target
	if target.Type != autoscalingv2.AverageValueMetricType || target.AverageValue == nil || target.AverageValue.MilliValue() != 100 {
		t.Fatalf("HPA target = %#v, want AverageValue 100m", target)
	}
	testutil.AssertVPAExists(t, vpaClient, manualVPA.Namespace, manualVPA.Name)
}

func TestReconcileDeploymentConvertsHPAForManualVhapeVPAOutsideWatcherScope(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	dep.Spec.Template.Spec.Containers = []corev1.Container{{
		Name: "api",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		}},
	}}
	policyName := "manual-policy"
	policy := &vhapev1alpha1.VhapePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName},
		Spec:       vhapev1alpha1.VhapePolicySpec{ManageHPA: true},
	}
	utilization := int32(50)
	hpa := testutil.NewHPA("api-hpa", dep.Namespace, dep.Name)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &utilization,
			},
		},
	}}
	manualVPA := testutil.NewVPA("manual-api", dep.Namespace, dep.Name)
	manualVPA.Annotations = map[string]string{vpaservice.VhapePolicyAnnotation: policyName}
	manualVPA.Labels = map[string]string{vpaservice.VhapeLabel: "team-a-recommender"}

	vpaClient, informers := testutil.NewInformers(
		t,
		[]*appsv1.Deployment{dep},
		nil,
		nil,
		nil,
		nil,
		nil,
		[]*vpav1.VerticalPodAutoscaler{manualVPA},
		[]*vhapev1alpha1.VhapePolicy{policy},
	)
	testutil.AddToIndexer(t, informers.HPA.Informer().GetIndexer(), hpa)
	kubeClient := kubefake.NewSimpleClientset(hpa.DeepCopy())
	hpaService, err := hpaservice.NewHPAService(informers.HPA, kubeClient)
	if err != nil {
		t.Fatalf("NewHPAService() error = %v", err)
	}
	r, err := New(
		informers.Deployment.Lister(),
		newScopeResolver(t, informers),
		hpaService,
		newVPAService(t, informers, vpaClient),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() error = %v", err)
	}

	updated, err := kubeClient.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated HPA: %v", err)
	}
	target := updated.Spec.Metrics[0].Resource.Target
	if target.Type != autoscalingv2.AverageValueMetricType || target.AverageValue == nil || target.AverageValue.MilliValue() != 100 {
		t.Fatalf("HPA target = %#v, want AverageValue 100m", target)
	}
	testutil.AssertVPAExists(t, vpaClient, manualVPA.Namespace, manualVPA.Name)
}

func TestReconcileDeploymentUsesLatestVhapeVPAPolicyForHPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	dep.Spec.Template.Spec.Containers = []corev1.Container{{
		Name: "api",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		}},
	}}
	utilization := int32(50)
	hpa := testutil.NewHPA("api-hpa", dep.Namespace, dep.Name)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &utilization,
			},
		},
	}}
	olderVPA := testutil.NewVPA("older", dep.Namespace, dep.Name)
	olderVPA.CreationTimestamp = metav1.NewTime(time.Unix(100, 0))
	olderVPA.Annotations = map[string]string{vpaservice.VhapePolicyAnnotation: "enabled-policy"}
	olderVPA.Labels = map[string]string{vpaservice.VhapeLabel: "older-recommender"}
	newerVPA := testutil.NewVPA("newer", dep.Namespace, dep.Name)
	newerVPA.CreationTimestamp = metav1.NewTime(time.Unix(200, 0))
	newerVPA.Annotations = map[string]string{vpaservice.VhapePolicyAnnotation: "disabled-policy"}
	newerVPA.Labels = map[string]string{vpaservice.VhapeLabel: "newer-recommender"}
	policies := []*vhapev1alpha1.VhapePolicy{
		{ObjectMeta: metav1.ObjectMeta{Name: "enabled-policy"}, Spec: vhapev1alpha1.VhapePolicySpec{ManageHPA: true}},
		{ObjectMeta: metav1.ObjectMeta{Name: "disabled-policy"}},
	}

	vpaClient, informers := testutil.NewInformers(
		t,
		[]*appsv1.Deployment{dep},
		nil,
		nil,
		nil,
		nil,
		nil,
		[]*vpav1.VerticalPodAutoscaler{newerVPA, olderVPA},
		policies,
	)
	testutil.AddToIndexer(t, informers.HPA.Informer().GetIndexer(), hpa)
	kubeClient := kubefake.NewSimpleClientset(hpa.DeepCopy())
	hpaService, err := hpaservice.NewHPAService(informers.HPA, kubeClient)
	if err != nil {
		t.Fatalf("NewHPAService() error = %v", err)
	}
	r, err := New(
		informers.Deployment.Lister(),
		newScopeResolver(t, informers),
		hpaService,
		newVPAService(t, informers, vpaClient),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := r.ReconcileDeployment(ctx, dep.Namespace, dep.Name); err != nil {
		t.Fatalf("ReconcileDeployment() error = %v", err)
	}

	updated, err := kubeClient.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get HPA: %v", err)
	}
	if got := updated.Spec.Metrics[0].Resource.Target.Type; got != autoscalingv2.UtilizationMetricType {
		t.Fatalf("HPA target type = %q, want %q from newest VPA policy", got, autoscalingv2.UtilizationMetricType)
	}
}

func TestReconcileDeploymentPreservesManualVPAAndDeletesGeneratedVPA(t *testing.T) {
	ctx := context.Background()
	dep := testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName)
	watched := testutil.NewWatchedNamespace(dep.Namespace)
	options := generationOptions(testutil.TestPolicyName, vpav1.UpdateModeInitial)
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
	options := generationOptions(testutil.TestPolicyName, testutil.TestVPAUpdateMode)
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
	oldOptions := generationOptions("old-policy", vpav1.UpdateModeRecreate)
	newOptions := generationOptions(testutil.TestPolicyName, testutil.TestVPAUpdateMode)
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
		nil,
	)

	scopeResolver := newScopeResolver(t, informers)
	vpaService := newVPAService(t, informers, client)

	hpaService := newHPAService(t, informers)
	r, err := New(informers.Deployment.Lister(), scopeResolver, hpaService, vpaService)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return r, client
}

func newScopeResolver(t *testing.T, informers *watcherinformers.Informers) *watcherscope.Scope {
	t.Helper()

	scopeResolver, err := watcherscope.New(informers)
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

func newHPAService(t *testing.T, informers *watcherinformers.Informers) *hpaservice.HPAService {
	t.Helper()

	service, err := hpaservice.NewHPAService(informers.HPA, kubefake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewHPAService() returned error: %v", err)
	}

	return service
}

func generationOptions(policyName string, updateMode vpav1.UpdateMode) vhapev1alpha1.VhapeWatchedNamespaceSpec {
	return vhapev1alpha1.VhapeWatchedNamespaceSpec{
		VhapePolicyName: policyName,
		VPAUpdateMode:   updateMode,
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
