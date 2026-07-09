package informers

import (
	vpalisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.k8s.io/v1"
	vhapelisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.vhape.io/v1alpha1"
	appslisters "k8s.io/client-go/listers/apps/v1"
)

// Listers groups cache-backed listers used by VHAPE Watcher.
type Listers struct {
	Deployment            appslisters.DeploymentLister
	VPA                   vpalisters.VerticalPodAutoscalerLister
	VhapeWatchedNamespace vhapelisters.VhapeWatchedNamespaceLister
	VhapeIgnoredWorkload  vhapelisters.VhapeIgnoredWorkloadLister
}

// Listers returns all listers backed by the informer caches.
func (i *Informers) Listers() Listers {
	return Listers{
		Deployment:            i.Deployment.Lister(),
		VPA:                   i.VPA.Lister(),
		VhapeWatchedNamespace: i.VhapeWatchedNamespace.Lister(),
		VhapeIgnoredWorkload:  i.VhapeIgnoredWorkload.Lister(),
	}
}
