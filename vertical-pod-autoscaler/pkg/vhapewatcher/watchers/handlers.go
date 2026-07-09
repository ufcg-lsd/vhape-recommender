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

func (w *Watchers) onDeploymentAdd(obj interface{}) {
	dep, ok := deploymentFromObject(obj)
	if !ok {
		return
	}

	w.sink.EnqueueDeployment(dep.Namespace, dep.Name)
}

func (w *Watchers) onDeploymentUpdate(_, _ interface{}) {
	// Nothing to do.
	//
	// VHAPE Watcher does not currently use mutable Deployment fields such as
	// labels, annotations, replicas, pod template or status to decide whether a
	// Deployment should be managed.
}

func (w *Watchers) onDeploymentDelete(_ interface{}) {
	// Nothing to do.
	//
	// VPAs created by VHAPE Watcher should have an ownerReference pointing to the
	// Deployment. When the Deployment is deleted, Kubernetes garbage collection
	// is responsible for deleting the managed VPA.
}

func (w *Watchers) onVPAAdd(obj interface{}) {
	vpa, ok := vpaFromObject(obj)
	if !ok {
		return
	}

	w.enqueueDeploymentFromVPA(vpa)
}

func (w *Watchers) onVPAUpdate(_, _ interface{}) {
	// Nothing to do.
	//
	// VHAPE Watcher does not depend on VPA updates to decide desired state.
	// VPA Add events are enough to detect newly created VPAs that may conflict
	// with watched workloads, and VPA Delete events are enough to detect managed
	// VPAs that may need to be recreated.
}

func (w *Watchers) onVPADelete(obj interface{}) {
	vpa, ok := vpaFromObject(obj)
	if !ok {
		return
	}

	w.enqueueDeploymentFromVPA(vpa)
}

func (w *Watchers) onWatchedNamespaceAdd(obj interface{}) {
	watched, ok := watchedNamespaceFromObject(obj)
	if !ok {
		return
	}

	w.sink.EnqueueDeploymentsInNamespace(watched.Name)
}

func (w *Watchers) onWatchedNamespaceUpdate(_, _ interface{}) {
	// Nothing to do.
	//
	// VhapeWatchedNamespace currently has no spec.
}

func (w *Watchers) onWatchedNamespaceDelete(obj interface{}) {
	watched, ok := watchedNamespaceFromObject(obj)
	if !ok {
		return
	}

	w.sink.EnqueueDeploymentsInNamespace(watched.Name)
}

func (w *Watchers) onIgnoredWorkloadAdd(obj interface{}) {
	ignored, ok := ignoredWorkloadFromObject(obj)
	if !ok {
		return
	}

	w.enqueueDeploymentFromIgnoredWorkload(ignored)
}

func (w *Watchers) onIgnoredWorkloadUpdate(_, _ interface{}) {
	// Nothing to do.
	//
	// VHAPE Watcher treats VhapeIgnoredWorkload as a declarative marker whose
	// relevant lifecycle is Add/Delete.
}

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