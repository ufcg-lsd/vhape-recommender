package watchers

import (
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

const (
	deploymentAPIVersion = "apps/v1"
	deploymentKind       = "Deployment"
)

// A new Deployment may require a generated VPA if its namespace is watched and
// the workload is not ignored.
func (w *Watchers) onDeploymentAdd(obj interface{}) {
	dep, ok := deploymentFromObject(obj)
	if !ok {
		return
	}

	w.sink.EnqueueDeployment(dep.Namespace, dep.Name)
}

// Deployment updates are ignored for now because the watcher only uses the
// Deployment identity. The relevant fields are namespace and name, which do not
// change during an update.
func (w *Watchers) onDeploymentUpdate(_, _ interface{}) {
	// Nothing to do.
}

// Deployment deletes are ignored because generated VPAs are expected to have an
// ownerReference pointing to the Deployment. Kubernetes garbage collection is
// responsible for deleting those VPAs.
func (w *Watchers) onDeploymentDelete(_ interface{}) {
	// Nothing to do.
}

// A new VPA may create a conflict with a generated VPA or with a watched Deployment.
func (w *Watchers) onVPAAdd(obj interface{}) {
	vpa, ok := vpaFromObject(obj)
	if !ok {
		return
	}

	w.enqueueDeploymentFromVPA(vpa)
}

// A VPA update may change its targetRef. Enqueue both the old and the new target
// identities so the reconciler can evaluate the current desired state for each
// affected Deployment.
func (w *Watchers) onVPAUpdate(oldObj, newObj interface{}) {
	oldVPA, ok := vpaFromObject(oldObj)
	if ok {
		w.enqueueDeploymentFromVPA(oldVPA)
	}

	newVPA, ok := vpaFromObject(newObj)
	if ok {
		w.enqueueDeploymentFromVPA(newVPA)
	}
}

// If a VPA is deleted and the target Deployment is still in scope, the
// reconciler may recreate the generated VPA.
func (w *Watchers) onVPADelete(obj interface{}) {
	vpa, ok := vpaFromObject(obj)
	if !ok {
		return
	}

	w.enqueueDeploymentFromVPA(vpa)
}

// When a namespace becomes watched, every Deployment in that namespace may need
// a generated VPA.
func (w *Watchers) onWatchedNamespaceAdd(obj interface{}) {
	watched, ok := watchedNamespaceFromObject(obj)
	if !ok {
		return
	}

	w.sink.EnqueueDeploymentsInNamespace(watched.Name)
}

// Watched namespace updates are ignored because the watcher depends only on
// metadata.name, which is immutable.
func (w *Watchers) onWatchedNamespaceUpdate(_, _ interface{}) {
	// Nothing to do.
}

// When a namespace stops being watched, all Deployments in that namespace are
// reconciled so managed VPAs can be cleaned up.
func (w *Watchers) onWatchedNamespaceDelete(obj interface{}) {
	watched, ok := watchedNamespaceFromObject(obj)
	if !ok {
		return
	}

	w.sink.EnqueueDeploymentsInNamespace(watched.Name)
}

// When a workload becomes ignored, the associated Deployment must be reconciled
// so any generated VPA can be cleaned up according to the watcher policy.
func (w *Watchers) onIgnoredWorkloadAdd(obj interface{}) {
	ignored, ok := ignoredWorkloadFromObject(obj)
	if !ok {
		return
	}

	w.enqueueDeploymentFromIgnoredWorkload(ignored)
}

// An ignored workload update may change its targetRef. Enqueue both the old and
// the new target identities so the reconciler can evaluate the current desired
// state for each affected Deployment.
func (w *Watchers) onIgnoredWorkloadUpdate(oldObj, newObj interface{}) {
	oldIgnored, ok := ignoredWorkloadFromObject(oldObj)
	if ok {
		w.enqueueDeploymentFromIgnoredWorkload(oldIgnored)
	}

	newIgnored, ok := ignoredWorkloadFromObject(newObj)
	if ok {
		w.enqueueDeploymentFromIgnoredWorkload(newIgnored)
	}
}

// When a workload stops being ignored, the target Deployment may need a generated
// VPA if it is still in a watched namespace.
func (w *Watchers) onIgnoredWorkloadDelete(obj interface{}) {
	ignored, ok := ignoredWorkloadFromObject(obj)
	if !ok {
		return
	}

	w.enqueueDeploymentFromIgnoredWorkload(ignored)
}

func (w *Watchers) enqueueDeploymentFromVPA(vpa *vpav1.VerticalPodAutoscaler) {
	if vpa == nil || vpa.Spec.TargetRef == nil {
		return
	}

	ref := vpa.Spec.TargetRef
	if !isDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
		return
	}

	w.sink.EnqueueDeployment(vpa.Namespace, ref.Name)
}

func (w *Watchers) enqueueDeploymentFromIgnoredWorkload(ignored *vhapev1alpha1.VhapeIgnoredWorkload) {
	if ignored == nil {
		return
	}

	ref := ignored.Spec.TargetRef
	if !isDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
		return
	}
	if ref.Namespace == "" {
		return
	}

	w.sink.EnqueueDeployment(ref.Namespace, ref.Name)
}

func isDeploymentTarget(apiVersion, kind, name string) bool {
	return apiVersion == deploymentAPIVersion &&
		strings.EqualFold(kind, deploymentKind) &&
		name != ""
}

func deploymentFromObject(obj interface{}) (*appsv1.Deployment, bool) {
	dep, ok := obj.(*appsv1.Deployment)
	return dep, ok
}

func vpaFromObject(obj interface{}) (*vpav1.VerticalPodAutoscaler, bool) {
	if vpa, ok := obj.(*vpav1.VerticalPodAutoscaler); ok {
		return vpa, true
	}

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}

	vpa, ok := tombstone.Obj.(*vpav1.VerticalPodAutoscaler)
	return vpa, ok
}

func watchedNamespaceFromObject(obj interface{}) (*vhapev1alpha1.VhapeWatchedNamespace, bool) {
	if watched, ok := obj.(*vhapev1alpha1.VhapeWatchedNamespace); ok {
		return watched, true
	}

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}

	watched, ok := tombstone.Obj.(*vhapev1alpha1.VhapeWatchedNamespace)
	return watched, ok
}

func ignoredWorkloadFromObject(obj interface{}) (*vhapev1alpha1.VhapeIgnoredWorkload, bool) {
	if ignored, ok := obj.(*vhapev1alpha1.VhapeIgnoredWorkload); ok {
		return ignored, true
	}

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}

	ignored, ok := tombstone.Obj.(*vhapev1alpha1.VhapeIgnoredWorkload)
	return ignored, ok
}