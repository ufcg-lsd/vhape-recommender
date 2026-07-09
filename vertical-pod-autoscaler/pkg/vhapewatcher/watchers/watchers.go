package watchers

import (
	"fmt"

	autoscalinginformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	vhapev1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.vhape.io/v1alpha1"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/tools/cache"
)

// DeploymentSink receives deployment reconciliation requests produced by watchers.
// The reconciler should implement this interface.
type DeploymentSink interface {
	EnqueueDeployment(namespace, name string)
	EnqueueDeploymentsInNamespace(namespace string)
}

// Watchers registers event handlers on informers.
type Watchers struct {
	sink DeploymentSink
}

// New creates a Watchers instance.
func New(sink DeploymentSink) (*Watchers, error) {
	if sink == nil {
		return nil, fmt.Errorf("deployment sink is nil")
	}

	return &Watchers{sink: sink}, nil
}

// Register connects all informer events to the deployment sink.
func (w *Watchers) Register(
	deploymentInformer appsinformers.DeploymentInformer,
	vpaInformer autoscalinginformers.VerticalPodAutoscalerInformer,
	watchedNamespaceInformer vhapev1alpha1informers.VhapeWatchedNamespaceInformer,
	ignoredWorkloadInformer vhapev1alpha1informers.VhapeIgnoredWorkloadInformer,
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

	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onDeploymentAdd,
		UpdateFunc: w.onDeploymentUpdate,
		DeleteFunc: w.onDeploymentDelete,
	})

	vpaInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onVPAAdd,
		UpdateFunc: w.onVPAUpdate,
		DeleteFunc: w.onVPADelete,
	})

	watchedNamespaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onWatchedNamespaceAdd,
		UpdateFunc: w.onWatchedNamespaceUpdate,
		DeleteFunc: w.onWatchedNamespaceDelete,
	})

	ignoredWorkloadInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onIgnoredWorkloadAdd,
		UpdateFunc: w.onIgnoredWorkloadUpdate,
		DeleteFunc: w.onIgnoredWorkloadDelete,
	})

	return nil
}
