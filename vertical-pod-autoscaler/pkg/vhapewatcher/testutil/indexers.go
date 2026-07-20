package testutil

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"

	appsv1 "k8s.io/api/apps/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpafake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vhapelisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.vhape.io/v1alpha1"
)

func NewIndexer() cache.Indexer {
	return cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
}

func NewNamespacedIndexer() cache.Indexer {
	return cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
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

func NewDeploymentLister(t *testing.T, deployments ...*appsv1.Deployment) appslisters.DeploymentLister {
	t.Helper()

	indexer := NewNamespacedIndexer()
	for _, dep := range deployments {
		AddToIndexer(t, indexer, dep)
	}

	return appslisters.NewDeploymentLister(indexer)
}

func NewVhapeListers(
	t *testing.T,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
) (vhapelisters.VhapeWatchedNamespaceLister, vhapelisters.VhapeIgnoredWorkloadLister) {
	t.Helper()

	watchedIndexer := NewIndexer()
	ignoredIndexer := NewIndexer()

	for _, watched := range watchedNamespaces {
		AddToIndexer(t, watchedIndexer, watched)
	}
	for _, ignored := range ignoredWorkloads {
		AddToIndexer(t, ignoredIndexer, ignored)
	}

	return vhapelisters.NewVhapeWatchedNamespaceLister(watchedIndexer),
		vhapelisters.NewVhapeIgnoredWorkloadLister(ignoredIndexer)
}

func NewVPAClientset(vpas ...*vpav1.VerticalPodAutoscaler) *vpafake.Clientset {
	return vpafake.NewSimpleClientset(ToRuntimeObjects(vpas...)...)
}

func ToRuntimeObjects(vpas ...*vpav1.VerticalPodAutoscaler) []runtime.Object {
	objects := make([]runtime.Object, 0, len(vpas))
	for _, vpa := range vpas {
		if vpa != nil {
			objects = append(objects, vpa.DeepCopy())
		}
	}
	return objects
}
