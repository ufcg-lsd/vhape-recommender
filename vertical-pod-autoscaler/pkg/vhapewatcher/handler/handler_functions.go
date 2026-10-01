package handler

import (
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// A new Deployment may require a generated VPA if its namespace is watched and
// the workload is not ignored.
func (h *Handler) onDeploymentAdd(obj interface{}) {
	dep, ok := deploymentFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring Deployment add event with unexpected object type")
		return
	}

	klog.V(4).InfoS("Enqueuing Deployment after add event", "deployment", klog.KObj(dep))
	h.sink.EnqueueDeployment(dep.Namespace, dep.Name)
}

// A Deployment update may change the requests used to convert utilization-based
// HPA targets. Reconcile the current object even when the generated VPA itself
// would remain unchanged.
func (h *Handler) onDeploymentUpdate(_, newObj interface{}) {
	dep, ok := deploymentFromObject(newObj)
	if !ok {
		klog.V(4).InfoS("Ignoring Deployment update event with unexpected new object type")
		return
	}

	klog.V(4).InfoS("Enqueuing Deployment after update event", "deployment", klog.KObj(dep))
	h.sink.EnqueueDeployment(dep.Namespace, dep.Name)
}

// Deployment deletes are ignored. If a Deployment is replaced with the same
// namespace/name and a new UID, the old generated VPA should be removed by
// Kubernetes garbage collection through ownerReferences. The VPA delete event
// will enqueue the Deployment again and the reconciler will create a new
// generated VPA if needed.
func (h *Handler) onDeploymentDelete(_ interface{}) {
	klog.V(4).InfoS("Ignoring Deployment delete event")
}

// A new HPA may affect how its target Deployment is managed.
func (h *Handler) onHPAAdd(obj interface{}) {
	klog.V(4).InfoS("HPA add event")

	hpa, ok := hpaFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring HPA add event with unexpected object type")
		return
	}

	h.enqueueDeploymentFromHPA(hpa)
}

// An HPA update may change its scaleTargetRef. Enqueue both the old and new
// target identities so the reconciler can evaluate each affected Deployment.
func (h *Handler) onHPAUpdate(oldObj, newObj interface{}) {
	klog.V(4).InfoS("HPA update event")

	oldHPA, ok := hpaFromObject(oldObj)
	if ok {
		h.enqueueDeploymentFromHPA(oldHPA)
	} else {
		klog.V(4).InfoS("Ignoring old object from HPA update event with unexpected object type")
	}

	newHPA, ok := hpaFromObject(newObj)
	if ok {
		h.enqueueDeploymentFromHPA(newHPA)
	} else {
		klog.V(4).InfoS("Ignoring new object from HPA update event with unexpected object type")
	}
}

// HPA deletion does not require reconciling its target Deployment.
func (h *Handler) onHPADelete(_ interface{}) {
	klog.V(4).InfoS("Ignoring HPA delete event")
}

// A new VPA may create a conflict with a generated VPA or with a watched Deployment.
func (h *Handler) onVPAAdd(obj interface{}) {
	klog.V(4).InfoS("VPA add event")

	vpa, ok := vpaFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VPA add event with unexpected object type")
		return
	}

	h.enqueueDeploymentFromVPA(vpa)
}

// A VPA update may change its targetRef. Enqueue both the old and the new target
// identities so the reconciler can evaluate the current desired state for each
// affected Deployment.
func (h *Handler) onVPAUpdate(oldObj, newObj interface{}) {
	klog.V(4).InfoS("VPA update event")

	oldVPA, ok := vpaFromObject(oldObj)
	if ok {
		h.enqueueDeploymentFromVPA(oldVPA)
	} else {
		klog.V(4).InfoS("Ignoring old object from VPA update event with unexpected object type")
	}

	newVPA, ok := vpaFromObject(newObj)
	if ok {
		h.enqueueDeploymentFromVPA(newVPA)
	} else {
		klog.V(4).InfoS("Ignoring new object from VPA update event with unexpected object type")
	}
}

// If a VPA is deleted and the target Deployment is still in scope, the
// reconciler may recreate the generated VPA.
func (h *Handler) onVPADelete(obj interface{}) {
	klog.V(4).InfoS("VPA deletion event")

	vpa, ok := vpaFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VPA delete event with unexpected object type")
		return
	}

	h.enqueueDeploymentFromVPA(vpa)
}

// When a namespace becomes watched, every Deployment in that namespace may need
// a generated VPA.
func (h *Handler) onWatchedNamespaceAdd(obj interface{}) {
	watched, ok := watchedNamespaceFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeWatchedNamespace add event with unexpected object type")
		return
	}

	klog.InfoS("Watched namespace added; enqueuing Deployments in namespace", "namespace", watched.Name)
	h.sink.EnqueueDeploymentsInNamespace(watched.Name)
}

// Watched namespace updates may change the assigned policy or the updatemode.
func (h *Handler) onWatchedNamespaceUpdate(_, newObj interface{}) {
	klog.V(4).InfoS("VhapeWatchedNamespace update event")

	watched, ok := watchedNamespaceFromObject(newObj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeWatchedNamespace update event with unexpected object type")
		return
	}

	klog.InfoS("Watched namespace updated; enqueuing Deployments in namespace", "namespace", watched.Name)
	h.sink.EnqueueDeploymentsInNamespace(watched.Name)
}

// When a namespace stops being watched, all Deployments in that namespace are
// reconciled so managed VPAs can be cleaned up.
func (h *Handler) onWatchedNamespaceDelete(obj interface{}) {
	watched, ok := watchedNamespaceFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeWatchedNamespace delete event with unexpected object type")
		return
	}

	klog.InfoS("Watched namespace deleted; enqueuing Deployments in namespace", "namespace", watched.Name)
	h.sink.EnqueueDeploymentsInNamespace(watched.Name)
}

// When a namespace regex starts watching namespaces, all Deployments in the
// namespaces matched by that regex may need reconciliation.
func (h *Handler) onWatchedNamespaceRegexAdd(obj interface{}) {
	watchedRegex, ok := watchedNamespaceRegexFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeWatchedNamespaceRegex add event with unexpected object type")
		return
	}

	klog.InfoS("Watched namespace regex added; enqueuing Deployments in matching namespaces", "regex", watchedRegex.Spec.Regex)
	h.sink.EnqueueDeploymentsMatchingNamespaceRegex(watchedRegex.Spec.Regex)
}

// A regex update may change either the matched namespace set or the desired
// configuration. Reconcile namespaces matched by the old regex and the new regex.
func (h *Handler) onWatchedNamespaceRegexUpdate(oldObj, newObj interface{}) {
	klog.V(4).InfoS("VhapeWatchedNamespaceRegex update event")

	oldWatchedRegex, oldOK := watchedNamespaceRegexFromObject(oldObj)
	if oldOK {
		h.sink.EnqueueDeploymentsMatchingNamespaceRegex(oldWatchedRegex.Spec.Regex)
	} else {
		klog.V(4).InfoS("Ignoring old object from VhapeWatchedNamespaceRegex update event with unexpected object type")
	}

	newWatchedRegex, newOK := watchedNamespaceRegexFromObject(newObj)
	if !newOK {
		klog.V(4).InfoS("Ignoring new object from VhapeWatchedNamespaceRegex update event with unexpected object type")
		return
	}

	if !oldOK || oldWatchedRegex.Spec.Regex != newWatchedRegex.Spec.Regex {
		h.sink.EnqueueDeploymentsMatchingNamespaceRegex(newWatchedRegex.Spec.Regex)
	}
}

// When a namespace regex is deleted, Deployments in namespaces matched by the
// deleted regex are reconciled so generated VPAs can be cleaned up if needed.
func (h *Handler) onWatchedNamespaceRegexDelete(obj interface{}) {
	watchedRegex, ok := watchedNamespaceRegexFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeWatchedNamespaceRegex delete event with unexpected object type")
		return
	}

	klog.InfoS("Watched namespace regex deleted; enqueuing Deployments in matching namespaces", "regex", watchedRegex.Spec.Regex)
	h.sink.EnqueueDeploymentsMatchingNamespaceRegex(watchedRegex.Spec.Regex)
}

// When a namespace becomes explicitly ignored, every Deployment in that
// namespace may need reconciliation.
func (h *Handler) onIgnoredNamespaceAdd(obj interface{}) {
	ignoredNamespace, ok := ignoredNamespaceFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeIgnoredNamespace add event with unexpected object type")
		return
	}

	klog.InfoS("Ignored namespace added; enqueuing Deployments in namespace", "namespace", ignoredNamespace.Name)
	h.sink.EnqueueDeploymentsInNamespace(ignoredNamespace.Name)
}

// VhapeIgnoredNamespace updates are ignored because the resource is a marker
// whose semantics depend only on metadata.name, which is immutable.
func (h *Handler) onIgnoredNamespaceUpdate(_, _ interface{}) {
	klog.V(4).InfoS("Ignoring VhapeIgnoredNamespace update event")
}

// When a namespace stops being explicitly ignored, its Deployments may become
// eligible through a regex rule and must be reconciled.
func (h *Handler) onIgnoredNamespaceDelete(obj interface{}) {
	ignoredNamespace, ok := ignoredNamespaceFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeIgnoredNamespace delete event with unexpected object type")
		return
	}

	klog.InfoS("Ignored namespace deleted; enqueuing Deployments in namespace", "namespace", ignoredNamespace.Name)
	h.sink.EnqueueDeploymentsInNamespace(ignoredNamespace.Name)
}

// When a workload becomes ignored, the associated Deployment must be reconciled
// so any generated VPA can be cleaned up according to the watcher policy.
func (h *Handler) onIgnoredWorkloadAdd(obj interface{}) {
	klog.V(4).InfoS("VhapeIgnoredWorkload add event")

	ignored, ok := ignoredWorkloadFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeIgnoredWorkload add event with unexpected object type")
		return
	}

	h.enqueueDeploymentFromIgnoredWorkload(ignored)
}

// An ignored workload update may change its targetRef. Enqueue both the old and
// the new target identities so the reconciler can evaluate the current desired
// state for each affected Deployment.
func (h *Handler) onIgnoredWorkloadUpdate(oldObj, newObj interface{}) {
	klog.V(4).InfoS("VhapeIgnoredWorkload update event")

	oldIgnored, ok := ignoredWorkloadFromObject(oldObj)
	if ok {
		h.enqueueDeploymentFromIgnoredWorkload(oldIgnored)
	} else {
		klog.V(4).InfoS("Ignoring old object from VhapeIgnoredWorkload update event with unexpected object type")
	}

	newIgnored, ok := ignoredWorkloadFromObject(newObj)
	if ok {
		h.enqueueDeploymentFromIgnoredWorkload(newIgnored)
	} else {
		klog.V(4).InfoS("Ignoring new object from VhapeIgnoredWorkload update event with unexpected object type")
	}
}

func (h *Handler) onVhapePolicyAdd(obj interface{}) {
	klog.V(4).InfoS("VhapePolicy add event")

	vhapePolicy, ok := vhapePolicyFromObj(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapePolicy add event with unexpected object type")
		return
	}

	h.enqueueDeploymentFromVhapePolicy(vhapePolicy)
}

// VhapePolicy specs are immutable, so an update cannot change the desired
// watcher configuration.
func (h *Handler) onVhapePolicyUpdate(_, _ interface{}) {
	klog.V(4).InfoS("Ignoring VhapePolicy update event")
}

// VhapePolicy deletion does not require reconciliation.
func (h *Handler) onVhapePolicyDelete(_ interface{}) {
	klog.V(4).InfoS("Ignoring VhapePolicy delete event")
}

// When a workload stops being ignored, the target Deployment may need a generated
// VPA if it is still in a watched namespace.
func (h *Handler) onIgnoredWorkloadDelete(obj interface{}) {
	klog.V(4).InfoS("VhapeIgnoredWorkload deletion event")

	ignored, ok := ignoredWorkloadFromObject(obj)
	if !ok {
		klog.V(4).InfoS("Ignoring VhapeIgnoredWorkload delete event with unexpected object type")
		return
	}

	h.enqueueDeploymentFromIgnoredWorkload(ignored)
}

func (h *Handler) enqueueDeploymentFromVPA(vpa *vpav1.VerticalPodAutoscaler) {
	if vpa == nil {
		return
	}
	if vpa.Spec.TargetRef == nil {
		klog.V(4).InfoS("Ignoring VPA event without targetRef", "vpa", klog.KObj(vpa))
		return
	}

	ref := vpa.Spec.TargetRef
	if !vpaservice.IsDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
		klog.V(4).InfoS("Ignoring VPA event for non-Deployment target", "vpa", klog.KObj(vpa), "apiVersion", ref.APIVersion, "kind", ref.Kind, "name", ref.Name)
		return
	}

	klog.V(4).InfoS("Enqueuing Deployment from VPA event", "deployment", klog.KRef(vpa.Namespace, ref.Name), "vpa", klog.KObj(vpa))
	h.sink.EnqueueDeployment(vpa.Namespace, ref.Name)
}

func (h *Handler) enqueueDeploymentFromHPA(hpa *autoscalingv2.HorizontalPodAutoscaler) {
	if hpa == nil {
		return
	}

	ref := hpa.Spec.ScaleTargetRef
	if !vpaservice.IsDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
		klog.V(4).InfoS("Ignoring HPA event for non-Deployment target", "hpa", klog.KObj(hpa), "apiVersion", ref.APIVersion, "kind", ref.Kind, "name", ref.Name)
		return
	}

	klog.V(4).InfoS("Enqueuing Deployment from HPA event", "deployment", klog.KRef(hpa.Namespace, ref.Name), "hpa", klog.KObj(hpa))
	h.sink.EnqueueDeployment(hpa.Namespace, ref.Name)
}

func (h *Handler) enqueueDeploymentFromIgnoredWorkload(ignored *vhapev1alpha1.VhapeIgnoredWorkload) {
	if ignored == nil {
		return
	}

	ref := ignored.Spec.TargetRef
	if !vpaservice.IsDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
		klog.Warningf(
			"Ignoring VhapeIgnoredWorkload %q with unsupported or incomplete targetRef: apiVersion=%q kind=%q name=%q",
			ignored.Name,
			ref.APIVersion,
			ref.Kind,
			ref.Name,
		)
		return
	}
	if ref.Namespace == "" {
		klog.Warningf("Ignoring VhapeIgnoredWorkload %q with empty targetRef.namespace", ignored.Name)
		return
	}

	klog.V(4).InfoS(
		"Enqueuing Deployment from VhapeIgnoredWorkload event",
		"deployment", klog.KRef(ref.Namespace, ref.Name),
		"ignoredWorkload", klog.KObj(ignored),
		"ignoredReason", ignored.Spec.Reason,
	)
	h.sink.EnqueueDeployment(ref.Namespace, ref.Name)
}

func (h *Handler) enqueueDeploymentFromVhapePolicy(vhapePolicy *vhapev1alpha1.VhapePolicy) {
	if vhapePolicy == nil {
		return
	}

	if h.vpaIndexer == nil {
		klog.ErrorS(nil, "Cannot enqueue Deployments from VhapePolicy event without a VPA index", "vhapePolicy", klog.KObj(vhapePolicy))
		return
	}

	items, err := h.vpaIndexer.ByIndex(watcherinformers.VPAByVhapePolicyIndex, vhapePolicy.Name)
	if err != nil {
		klog.ErrorS(err, "List VPAs by VhapePolicy index", "vhapePolicy", klog.KObj(vhapePolicy))
		return
	}

	for _, item := range items {
		vpa, ok := item.(*vpav1.VerticalPodAutoscaler)
		if !ok {
			klog.V(4).InfoS("Ignoring non-VPA object returned by VhapePolicy index", "vhapePolicy", klog.KObj(vhapePolicy))
			continue
		}
		if !vpaservice.HasVhapeRecommenderLabel(vpa) {
			klog.V(4).InfoS("Ignoring VPA without the VHAPE recommender label", "vpa", klog.KObj(vpa), "vhapePolicy", klog.KObj(vhapePolicy))
			continue
		}

		h.enqueueDeploymentFromVPA(vpa)
	}
}

func deploymentFromObject(obj interface{}) (*appsv1.Deployment, bool) {
	dep, ok := obj.(*appsv1.Deployment)
	return dep, ok
}

func hpaFromObject(obj interface{}) (*autoscalingv2.HorizontalPodAutoscaler, bool) {
	hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler)
	return hpa, ok
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

func watchedNamespaceRegexFromObject(obj interface{}) (*vhapev1alpha1.VhapeWatchedNamespaceRegex, bool) {
	if watchedRegex, ok := obj.(*vhapev1alpha1.VhapeWatchedNamespaceRegex); ok {
		return watchedRegex, true
	}

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}

	watchedRegex, ok := tombstone.Obj.(*vhapev1alpha1.VhapeWatchedNamespaceRegex)
	return watchedRegex, ok
}

func ignoredNamespaceFromObject(obj interface{}) (*vhapev1alpha1.VhapeIgnoredNamespace, bool) {
	if ignoredNamespace, ok := obj.(*vhapev1alpha1.VhapeIgnoredNamespace); ok {
		return ignoredNamespace, true
	}

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}

	ignoredNamespace, ok := tombstone.Obj.(*vhapev1alpha1.VhapeIgnoredNamespace)
	return ignoredNamespace, ok
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

func vhapePolicyFromObj(obj interface{}) (*vhapev1alpha1.VhapePolicy, bool) {
	if vhapePolicy, ok := obj.(*vhapev1alpha1.VhapePolicy); ok {
		return vhapePolicy, true
	}

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}

	vhapePolicy, ok := tombstone.Obj.(*vhapev1alpha1.VhapePolicy)
	return vhapePolicy, ok
}
