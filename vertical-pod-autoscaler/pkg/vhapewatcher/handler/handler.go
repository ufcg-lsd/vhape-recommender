package handler

import (
	"fmt"

	autoscalinginformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	vhapev1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.vhape.io/v1alpha1"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/tools/cache"
)

// DeploymentSink receives deployment reconciliation requests produced by handlers.
// The reconciler should implement this interface.
type DeploymentSink interface {
	EnqueueDeployment(namespace, name string)
	EnqueueDeploymentsInNamespace(namespace string)
}

// Handler registers event handlers on informers.
type Handler struct {
	sink DeploymentSink
}

// New creates a Handler instance.
func New(sink DeploymentSink) (*Handler, error) {
	if sink == nil {
		return nil, fmt.Errorf("deployment sink is nil")
	}

	return &Handler{sink: sink}, nil
}


// this interfaces is used to allow for testing the 
// handler functions association without the need of real informers.
type eventHandlerReceiver interface {
	AddEventHandler(cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error)
}

// defines the type that associates handler fuctions for each handled object
type informerHandlerFuncs struct {
	deployment       cache.ResourceEventHandlerFuncs
	vpa              cache.ResourceEventHandlerFuncs
	watchedNamespace cache.ResourceEventHandlerFuncs
	ignoredWorkload  cache.ResourceEventHandlerFuncs
}

// declaration of the appropriate handlers for each handled object
func (h *Handler) informerHandlerFuncs() informerHandlerFuncs {
	return informerHandlerFuncs{
		deployment: cache.ResourceEventHandlerFuncs{
			AddFunc:    h.onDeploymentAdd,
			UpdateFunc: h.onDeploymentUpdate,
			DeleteFunc: h.onDeploymentDelete,
		},
		vpa: cache.ResourceEventHandlerFuncs{
			AddFunc:    h.onVPAAdd,
			UpdateFunc: h.onVPAUpdate,
			DeleteFunc: h.onVPADelete,
		},
		watchedNamespace: cache.ResourceEventHandlerFuncs{
			AddFunc:    h.onWatchedNamespaceAdd,
			UpdateFunc: h.onWatchedNamespaceUpdate,
			DeleteFunc: h.onWatchedNamespaceDelete,
		},
		ignoredWorkload: cache.ResourceEventHandlerFuncs{
			AddFunc:    h.onIgnoredWorkloadAdd,
			UpdateFunc: h.onIgnoredWorkloadUpdate,
			DeleteFunc: h.onIgnoredWorkloadDelete,
		},
	}
}

// connects all informer events to the appropriate handlers.
func (h *Handler) RegisterHandlerFunctionsOnInformers(
	deploymentInformer appsinformers.DeploymentInformer,
	vpaInformer autoscalinginformers.VerticalPodAutoscalerInformer,
	watchedNamespaceInformer vhapev1alpha1informers.VhapeWatchedNamespaceInformer,
	ignoredWorkloadInformer vhapev1alpha1informers.VhapeIgnoredWorkloadInformer,
) error {
	return registerHandlersOnReceivers(
		deploymentInformer.Informer(),
		vpaInformer.Informer(),
		watchedNamespaceInformer.Informer(),
		ignoredWorkloadInformer.Informer(),
		h.informerHandlerFuncs(),
	)
}

// private method extracted for testing
func registerHandlersOnReceivers(
	deploymentInformer eventHandlerReceiver,
	vpaInformer eventHandlerReceiver,
	watchedNamespaceInformer eventHandlerReceiver,
	ignoredWorkloadInformer eventHandlerReceiver,
	handlers informerHandlerFuncs,
) error {
	if deploymentInformer == nil {
		return fmt.Errorf("deployment informer is nil")
	}
	if vpaInformer == nil {
		return fmt.Errorf("vpa informer is nil")
	}
	if watchedNamespaceInformer == nil {
		return fmt.Errorf("vhape watched namespace informer is nil")
	}
	if ignoredWorkloadInformer == nil {
		return fmt.Errorf("vhape ignored workload informer is nil")
	}

	deploymentInformer.AddEventHandler(handlers.deployment)
	vpaInformer.AddEventHandler(handlers.vpa)
	watchedNamespaceInformer.AddEventHandler(handlers.watchedNamespace)
	ignoredWorkloadInformer.AddEventHandler(handlers.ignoredWorkload)

	return nil
}
