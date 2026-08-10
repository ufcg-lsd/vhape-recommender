package testutil

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vhapeclient "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/client"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
)

func NewInformers(
	t *testing.T,
	deployments []*appsv1.Deployment,
	namespaces []*corev1.Namespace,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	watchedNamespaceRegexes []*vhapev1alpha1.VhapeWatchedNamespaceRegex,
	ignoredNamespaces []*vhapev1alpha1.VhapeIgnoredNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
	vpas []*vpav1.VerticalPodAutoscaler,
) (*vpafake.Clientset, *watcherinformers.Informers) {
	t.Helper()

	vhapeClient := vpafake.NewSimpleClientset()
	informers, err := watcherinformers.New(&vhapeclient.Clients{
		Kube:  kubefake.NewSimpleClientset(),
		Vhape: vhapeClient,
	})
	if err != nil {
		t.Fatalf("informers.New() returned error: %v", err)
	}

	namespaceIndexer := informers.Namespace.Informer().GetIndexer()
	for _, namespace := range namespaces {
		AddToIndexer(t, namespaceIndexer, namespace)
	}

	for _, dep := range deployments {
		AddToIndexer(t, informers.Deployment.Informer().GetIndexer(), dep)
		if dep == nil || dep.Namespace == "" {
			continue
		}

		// Deployment fixtures also populate the Namespace cache used by regex handlers.
		if _, exists, err := namespaceIndexer.GetByKey(dep.Namespace); err != nil {
			t.Fatalf("get namespace from indexer: %v", err)
		} else if !exists {
			AddToIndexer(t, namespaceIndexer, NewNamespace(dep.Namespace))
		}
	}
	for _, watched := range watchedNamespaces {
		AddToIndexer(t, informers.VhapeWatchedNamespace.Informer().GetIndexer(), watched)
	}
	for _, watchedRegex := range watchedNamespaceRegexes {
		AddToIndexer(t, informers.VhapeWatchedNamespaceRegex.Informer().GetIndexer(), watchedRegex)
	}
	for _, ignoredNamespace := range ignoredNamespaces {
		AddToIndexer(t, informers.VhapeIgnoredNamespace.Informer().GetIndexer(), ignoredNamespace)
	}
	for _, ignored := range ignoredWorkloads {
		AddToIndexer(t, informers.VhapeIgnoredWorkload.Informer().GetIndexer(), ignored)
	}
	for _, vpa := range vpas {
		AddToIndexer(t, informers.VPA.Informer().GetIndexer(), vpa)
		if vpa == nil {
			continue
		}

		// VPAService mutates VPAs through the client, so seed the fake client too.
		if err := vhapeClient.Tracker().Add(vpa.DeepCopy()); err != nil {
			t.Fatalf("add VPA to fake client: %v", err)
		}
	}

	return vhapeClient, informers
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
