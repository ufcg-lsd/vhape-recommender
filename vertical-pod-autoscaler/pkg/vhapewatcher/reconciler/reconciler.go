package reconciler

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	watcherscope "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/scope"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	queueName  = "vhape-watcher-deployments"
	maxRetries = 5
	reasonNotManagedVPAPresent = "not-managed-vpa-present"
)

type Reconciler struct {
	deploymentLister appslisters.DeploymentLister
	scope            *watcherscope.Scope
	vpaService       *vpaservice.VPAService

	queue workqueue.TypedRateLimitingInterface[string]
}

func New(
	deploymentLister appslisters.DeploymentLister,
	scopeResolver *watcherscope.Scope,
	vpaService *vpaservice.VPAService,
) (*Reconciler, error) {
	if deploymentLister == nil {
		return nil, fmt.Errorf("deployment lister is nil")
	}
	if scopeResolver == nil {
		return nil, fmt.Errorf("scope resolver is nil")
	}
	if vpaService == nil {
		return nil, fmt.Errorf("vpa service is nil")
	}

	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{
			Name: queueName,
		},
	)

	return &Reconciler{
		deploymentLister: deploymentLister,
		scope:            scopeResolver,
		vpaService:       vpaService,
		queue:            queue,
	}, nil
}

func (r *Reconciler) EnqueueDeployment(namespace, name string) {
	if namespace == "" || name == "" {
		klog.V(4).InfoS("Ignoring enqueue request with empty Deployment identity", "namespace", namespace, "name", name)
		return
	}

	key := namespacedKey(namespace, name)
	klog.V(5).InfoS("Enqueuing Deployment", "deployment", key)
	r.queue.Add(key)
}

func (r *Reconciler) EnqueueDeploymentsInNamespace(namespace string) {
	if namespace == "" {
		klog.V(4).InfoS("Ignoring namespace enqueue request with empty namespace")
		return
	}

	deployments, err := r.deploymentLister.
		Deployments(namespace).
		List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("list Deployments in namespace %q from cache: %w", namespace, err))
		return
	}

	for _, dep := range deployments {
		if dep == nil {
			continue
		}

		r.EnqueueDeployment(dep.Namespace, dep.Name)
	}
}

func (r *Reconciler) Run(ctx context.Context, workers int) {
	if workers < 1 {
		klog.Warningf("Invalid worker count %d; using 1 worker", workers)
		workers = 1
	}

	klog.InfoS("Starting VHAPE Watcher reconciler", "workers", workers)

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, r.runWorker, time.Second)
	}

	<-ctx.Done()

	klog.InfoS("Stopping VHAPE Watcher reconciler")

	r.queue.ShutDown()
}

func (r *Reconciler) runWorker(ctx context.Context) {
	defer utilruntime.HandleCrash()

	for {
		if shouldContinue := r.processNextWorkItem(ctx); !shouldContinue {
			return
		}
	}
}

func (r *Reconciler) processNextWorkItem(ctx context.Context) bool {
	key, queueClosed := r.queue.Get()
	if queueClosed {
		return false
	}

	defer r.queue.Done(key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid Deployment queue key %q: %w", key, err))
		r.queue.Forget(key)
		return true
	}

	klog.V(4).InfoS("Processing Deployment reconciliation", "deployment", key, "requeues", r.queue.NumRequeues(key))
	err = r.ReconcileDeployment(ctx, namespace, name)
	if err != nil {
		klog.ErrorS(err, "Error reconciling Deployment", "deployment", key, "requeues", r.queue.NumRequeues(key))

		if r.queue.NumRequeues(key) >= maxRetries {
			utilruntime.HandleError(fmt.Errorf("dropping Deployment %q after %d retries: %w", key, maxRetries, err))
			r.queue.Forget(key)
			return true
		}

		r.queue.AddRateLimited(key)
		return true
	}

	klog.V(4).InfoS("Finished Deployment reconciliation", "deployment", key)
	r.queue.Forget(key)
	return true
}

func (r *Reconciler) ReconcileDeployment(ctx context.Context, namespace string, name string) error {
	if namespace == "" {
		return fmt.Errorf("deployment namespace is empty")
	}
	if name == "" {
		return fmt.Errorf("deployment name is empty")
	}

	dep, err := r.deploymentLister.
		Deployments(namespace).
		Get(name)
	if apierrors.IsNotFound(err) {
		klog.V(3).InfoS("Deployment no longer exists; skipping reconciliation", "deployment", klog.KRef(namespace, name))
		return nil
	}
	if err != nil {
		return fmt.Errorf("get Deployment %q/%q from cache: %w", namespace, name, err)
	}

	decision, err := r.scope.ShouldManageDeployment(dep)
	if err != nil {
		return err
	}

	vpas, err := r.vpaService.ListForDeployment(dep)
	if err != nil {
		return err
	}

	var total int
	var notManagedCount int
	var managedCount int
	for _, vpa := range vpas {
		if vpa == nil {
			continue
		}

		total++

		if vpaservice.IsManagedByWatcher(vpa) {
			managedCount++
			continue
		}

		notManagedCount++
	}

	klog.V(4).InfoS(
		"Evaluated Deployment scope and associated VPAs",
		"deployment", klog.KObj(dep),
		"shouldManage", decision.ShouldManage,
		"reason", decision.Reason,
		"associatedVPAs", total,
		"generatedVPAs", managedCount,
		"notManagedVPAs", notManagedCount,
	)

	if total > 1 {
		klog.Warningf(
			"Deployment %q/%q has multiple associated VPAs: total=%d generated=%d notManaged=%d",
			dep.Namespace,
			dep.Name,
			total,
			managedCount,
			notManagedCount,
		)
	}

	if !decision.ShouldManage {
		klog.V(3).InfoS("Deployment is outside VHAPE Watcher scope; ensuring generated VPA is absent", "deployment", klog.KObj(dep), "reason", decision.Reason)
		return r.vpaService.EnsureNoGeneratedVPAForDeployment(ctx, vpas, decision.Reason)
	}

	if notManagedCount > 0 {
		klog.Warningf(
			"Deployment %q/%q is watched but has a not-managed VPA. Generated VPA will be removed if present and the not-managed VPA will be preserved.",
			dep.Namespace,
			dep.Name,
		)

		return r.vpaService.EnsureNoGeneratedVPAForDeployment(ctx, vpas, reasonNotManagedVPAPresent)
	}

	if managedCount > 0 {
		klog.V(3).InfoS("Generated VPA already exists; leaving it unchanged", "deployment", klog.KObj(dep), "generatedVPAs", managedCount)
		return nil
	}

	klog.InfoS("Deployment is watched and has no associated VPA; creating generated VPA", "deployment", klog.KObj(dep))
	return r.vpaService.CreateGeneratedVPAForDeployment(ctx, dep)
}

func namespacedKey(namespace, name string) string {
	return namespace + "/" + name
}
