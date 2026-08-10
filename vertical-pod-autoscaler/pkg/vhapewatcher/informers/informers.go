package informers

import (
	"fmt"

	vhapeinformerfactory "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
	kubeinformerfactory "k8s.io/client-go/informers"

	autoscalinginformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	vhapev1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.vhape.io/v1alpha1"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"

	vhapeclient "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/client"

	"k8s.io/client-go/tools/cache"
)

// Informers groups all shared informers used by VHAPE Watcher.
type Informers struct {
	kubeFactory  kubeinformerfactory.SharedInformerFactory
	vhapeFactory vhapeinformerfactory.SharedInformerFactory

	Deployment                 appsinformers.DeploymentInformer
	Namespace                  coreinformers.NamespaceInformer
	VPA                        autoscalinginformers.VerticalPodAutoscalerInformer
	VhapeWatchedNamespace      vhapev1alpha1informers.VhapeWatchedNamespaceInformer
	VhapeWatchedNamespaceRegex vhapev1alpha1informers.VhapeWatchedNamespaceRegexInformer
	VhapeIgnoredNamespace      vhapev1alpha1informers.VhapeIgnoredNamespaceInformer
	VhapeIgnoredWorkload       vhapev1alpha1informers.VhapeIgnoredWorkloadInformer
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

	namespaceInformer := kubeFactory.
		Core().
		V1().
		Namespaces()

	vpaInformer := vhapeFactory.
		Autoscaling().
		V1().
		VerticalPodAutoscalers()

	if err := AddDeploymentToVPAsIndex(vpaInformer); err != nil {
		return nil, fmt.Errorf("add Deployment to VPAs index: %w", err)
	}

	watchedNamespaceInformer := vhapeFactory.
		VhapeAutoscaling().
		V1alpha1().
		VhapeWatchedNamespaces()

	watchedNamespaceRegexInformer := vhapeFactory.
		VhapeAutoscaling().
		V1alpha1().
		VhapeWatchedNamespaceRegexes()

	ignoredNamespaceInformer := vhapeFactory.
		VhapeAutoscaling().
		V1alpha1().
		VhapeIgnoredNamespaces()

	ignoredWorkloadInformer := vhapeFactory.
		VhapeAutoscaling().
		V1alpha1().
		VhapeIgnoredWorkloads()

	if err := AddDeploymentToIgnoredWorkloadsIndex(ignoredWorkloadInformer); err != nil {
		return nil, fmt.Errorf("add Deployment to VhapeIgnoredWorkloads index: %w", err)
	}

	return &Informers{
		kubeFactory:                kubeFactory,
		vhapeFactory:               vhapeFactory,
		Deployment:                 deploymentInformer,
		Namespace:                  namespaceInformer,
		VPA:                        vpaInformer,
		VhapeWatchedNamespace:      watchedNamespaceInformer,
		VhapeWatchedNamespaceRegex: watchedNamespaceRegexInformer,
		VhapeIgnoredNamespace:      ignoredNamespaceInformer,
		VhapeIgnoredWorkload:       ignoredWorkloadInformer,
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
		i.Namespace.Informer().HasSynced,
		i.VPA.Informer().HasSynced,
		i.VhapeWatchedNamespace.Informer().HasSynced,
		i.VhapeWatchedNamespaceRegex.Informer().HasSynced,
		i.VhapeIgnoredNamespace.Informer().HasSynced,
		i.VhapeIgnoredWorkload.Informer().HasSynced,
	); !ok {
		return fmt.Errorf("failed to sync informer caches")
	}

	return nil
}
