package testutil

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformerfactory "k8s.io/client-go/informers"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vhapeinformerfactory "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions"
	autoscalinginformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	vhapev1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.vhape.io/v1alpha1"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
)

type Informers struct {
	Deployment            appsinformers.DeploymentInformer
	VPA                   autoscalinginformers.VerticalPodAutoscalerInformer
	VhapeWatchedNamespace vhapev1alpha1informers.VhapeWatchedNamespaceInformer
	VhapeIgnoredWorkload  vhapev1alpha1informers.VhapeIgnoredWorkloadInformer
}

func NewInformers(
	t *testing.T,
	deployments []*appsv1.Deployment,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
	vpas []*vpav1.VerticalPodAutoscaler,
) (*kubefake.Clientset, *vpafake.Clientset, *Informers) {
	t.Helper()

	kubeClient := kubefake.NewSimpleClientset(kubeObjects(deployments...)...)
	vhapeClient := vpafake.NewSimpleClientset(vhapeObjects(watchedNamespaces, ignoredWorkloads, vpas)...)

	kubeFactory := kubeinformerfactory.NewSharedInformerFactory(kubeClient, 0)
	vhapeFactory := vhapeinformerfactory.NewSharedInformerFactory(vhapeClient, 0)

	informers := &Informers{
		Deployment:            kubeFactory.Apps().V1().Deployments(),
		VPA:                   vhapeFactory.Autoscaling().V1().VerticalPodAutoscalers(),
		VhapeWatchedNamespace: vhapeFactory.VhapeAutoscaling().V1alpha1().VhapeWatchedNamespaces(),
		VhapeIgnoredWorkload:  vhapeFactory.VhapeAutoscaling().V1alpha1().VhapeIgnoredWorkloads(),
	}

	if err := vpaservice.AddDeploymentToVPAsIndex(informers.VPA); err != nil {
		t.Fatalf("AddDeploymentToVPAsIndex returned error: %v", err)
	}

	for _, dep := range deployments {
		AddToIndexer(t, informers.Deployment.Informer().GetIndexer(), dep)
	}
	for _, watched := range watchedNamespaces {
		AddToIndexer(t, informers.VhapeWatchedNamespace.Informer().GetIndexer(), watched)
	}
	for _, ignored := range ignoredWorkloads {
		AddToIndexer(t, informers.VhapeIgnoredWorkload.Informer().GetIndexer(), ignored)
	}
	for _, vpa := range vpas {
		AddToIndexer(t, informers.VPA.Informer().GetIndexer(), vpa)
	}

	return kubeClient, vhapeClient, informers
}

func kubeObjects(deployments ...*appsv1.Deployment) []runtime.Object {
	objects := make([]runtime.Object, 0, len(deployments))
	for _, dep := range deployments {
		if dep != nil {
			objects = append(objects, dep.DeepCopy())
		}
	}
	return objects
}

func vhapeObjects(
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
	vpas []*vpav1.VerticalPodAutoscaler,
) []runtime.Object {
	objects := make([]runtime.Object, 0, len(watchedNamespaces)+len(ignoredWorkloads)+len(vpas))

	for _, watched := range watchedNamespaces {
		if watched != nil {
			objects = append(objects, watched.DeepCopy())
		}
	}
	for _, ignored := range ignoredWorkloads {
		if ignored != nil {
			objects = append(objects, ignored.DeepCopy())
		}
	}
	for _, vpa := range vpas {
		if vpa != nil {
			objects = append(objects, vpa.DeepCopy())
		}
	}

	return objects
}

func AddToIndexer(t *testing.T, indexer cache.Indexer, obj interface{}) {
	t.Helper()

	if obj == nil {
		return
	}
	if err := indexer.Add(obj); err != nil {
		t.Fatalf("add object to indexer: %v", err)
	}
}
