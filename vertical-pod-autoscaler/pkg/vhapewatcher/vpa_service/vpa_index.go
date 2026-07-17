package vpaservice

import (
	"fmt"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpaInformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	"k8s.io/client-go/tools/cache"
)

const IndexName = "vhape.io/vpa-by-deployment"

// adds a Deployment to VPAs index to the VPA informer.
// this allows for mapping deployments to a list of VPAs that manage it.
func AddDeploymentToVPAsIndex(informer vpaInformers.VerticalPodAutoscalerInformer) error {
	if informer == nil {
		return fmt.Errorf("vpa informer is nil")
	}

	return informer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		IndexName: getAssociatedVPADeploymentKey,
	})
}

// returns the associated vpa deployment key
func getAssociatedVPADeploymentKey(obj interface{}) ([]string, error) {
	vpa, ok := obj.(*vpav1.VerticalPodAutoscaler)
	if !ok {
		return nil, nil
	}
	if vpa.Spec.TargetRef == nil {
		return nil, nil
	}

	ref := vpa.Spec.TargetRef
	if !IsDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
		return nil, nil
	}

	return []string{namespacedKey(vpa.Namespace, ref.Name)}, nil
}

func namespacedKey(namespace, name string) string {
	return namespace + "/" + name
}
