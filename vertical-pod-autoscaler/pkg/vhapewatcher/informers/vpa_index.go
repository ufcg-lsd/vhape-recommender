package informers

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpainformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	"k8s.io/client-go/tools/cache"
)

const VPAByDeploymentIndex = "vhape.io/vpa-by-deployment"

// AddDeploymentToVPAsIndex adds a Deployment-to-VPA index to the VPA informer.
func AddDeploymentToVPAsIndex(informer vpainformers.VerticalPodAutoscalerInformer) error {
	if informer == nil {
		return fmt.Errorf("vpa informer is nil")
	}

	return informer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		VPAByDeploymentIndex: GetAssociatedVPADeploymentKey,
	})
}

// GetAssociatedVPADeploymentKey returns the Deployment key targeted by a VPA.
func GetAssociatedVPADeploymentKey(obj interface{}) ([]string, error) {
	vpa, ok := obj.(*vpav1.VerticalPodAutoscaler)
	if !ok || vpa == nil || vpa.Spec.TargetRef == nil {
		return nil, nil
	}

	ref := vpa.Spec.TargetRef
	if ref.APIVersion != appsv1.SchemeGroupVersion.String() || ref.Kind != "Deployment" || ref.Name == "" {
		return nil, nil
	}

	return []string{NamespacedKey(vpa.Namespace, ref.Name)}, nil
}

func NamespacedKey(namespace, name string) string {
	return namespace + "/" + name
}
