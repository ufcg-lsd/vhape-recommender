package informers

import (
	"fmt"
	vhapeinformerfactory "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
	kubeinformerfactory "k8s.io/client-go/informers"
	
	appsinformers "k8s.io/client-go/informers/apps/v1"
	autoscalinginformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	vhapev1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.vhape.io/v1alpha1"
	
	vhapeclient "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/client"

	"k8s.io/client-go/tools/cache"
)

// Informers groups all shared informers used by VHAPE Watcher.
type Informers struct {
	kubeFactory  kubeinformerfactory.SharedInformerFactory
	vhapeFactory vhapeinformerfactory.SharedInformerFactory

	Deployment            appsinformers.DeploymentInformer
	VPA                   autoscalinginformers.VerticalPodAutoscalerInformer
	VhapeWatchedNamespace vhapev1alpha1informers.VhapeWatchedNamespaceInformer
	VhapeIgnoredWorkload  vhapev1alpha1informers.VhapeIgnoredWorkloadInformer
}

// New creates informers for native Kubernetes resources, VPA resources and VHAPE resources.
func New(clients *vhapeclient.Clients) (*Informers, error) {
	if clients == nil {
		return nil, fmt.Errorf("clients is nil")
	}
	if clients.Kube == nil {
		return nil, fmt.Errorf("kubernetes client is nil")
	}
	if clients.Vhape == nil {
		return nil, fmt.Errorf("vhape client is nil")
	}

	kubeFactory := kubeinformerfactory.NewSharedInformerFactory(clients.Kube, 0)
	vhapeFactory := vhapeinformerfactory.NewSharedInformerFactory(clients.Vhape, 0)

	deploymentInformer := kubeFactory.
		Apps().
		V1().
		Deployments()

	vpaInformer := vhapeFactory.
		Autoscaling().
		V1().
		VerticalPodAutoscalers()

	watchedNamespaceInformer := vhapeFactory.
		VhapeAutoscaling().
		V1alpha1().
		VhapeWatchedNamespaces()

	ignoredWorkloadInformer := vhapeFactory.
		VhapeAutoscaling().
		V1alpha1().
		VhapeIgnoredWorkloads()

	return &Informers{
		kubeFactory:           kubeFactory,
		vhapeFactory:          vhapeFactory,
		Deployment:            deploymentInformer,
		VPA:                   vpaInformer,
		VhapeWatchedNamespace: watchedNamespaceInformer,
		VhapeIgnoredWorkload:  ignoredWorkloadInformer,
	}, nil
}

// Start starts all informer factories.
func (i *Informers) Start(stopCh <-chan struct{}) {
	i.kubeFactory.Start(stopCh)
	i.vhapeFactory.Start(stopCh)
}

func (i *Informers) WaitForCacheSync(stopCh <-chan struct{}) error {
	if ok := cache.WaitForNamedCacheSync(
		"vhape-watcher",
		stopCh,
		i.Deployment.Informer().HasSynced,
		i.VPA.Informer().HasSynced,
		i.VhapeWatchedNamespace.Informer().HasSynced,
		i.VhapeIgnoredWorkload.Informer().HasSynced,
	); !ok {
		return fmt.Errorf("failed to sync informer caches")
	}

	return nil
}
