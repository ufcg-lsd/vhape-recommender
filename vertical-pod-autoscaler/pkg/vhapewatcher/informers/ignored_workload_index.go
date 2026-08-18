package informers

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vhapeinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.vhape.io/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

const IgnoredWorkloadByDeploymentIndex = "vhape.io/ignored-workload-by-deployment"

// AddDeploymentToIgnoredWorkloadsIndex adds a Deployment-to-VhapeIgnoredWorkload
// index to the VhapeIgnoredWorkload informer.
func AddDeploymentToIgnoredWorkloadsIndex(informer vhapeinformers.VhapeIgnoredWorkloadInformer) error {
	if informer == nil {
		return fmt.Errorf("vhape ignored workload informer is nil")
	}

	return informer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		IgnoredWorkloadByDeploymentIndex: GetAssociatedIgnoredWorkloadDeploymentKey,
	})
}

// GetAssociatedIgnoredWorkloadDeploymentKey returns the Deployment key targeted
// by a VhapeIgnoredWorkload.
func GetAssociatedIgnoredWorkloadDeploymentKey(obj interface{}) ([]string, error) {
	ignoredWorkload, ok := obj.(*vhapev1alpha1.VhapeIgnoredWorkload)
	if !ok || ignoredWorkload == nil {
		return nil, nil
	}

	ref := ignoredWorkload.Spec.TargetRef
	if ref.APIVersion != appsv1.SchemeGroupVersion.String() ||
		ref.Kind != "Deployment" ||
		ref.Namespace == "" ||
		ref.Name == "" {
		return nil, nil
	}

	return []string{NamespacedKey(ref.Namespace, ref.Name)}, nil
}
