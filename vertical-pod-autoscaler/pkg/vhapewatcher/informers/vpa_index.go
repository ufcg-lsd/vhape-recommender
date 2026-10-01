package informers

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpainformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	VPAByDeploymentIndex  = "vhape.io/vpa-by-deployment"
	VPAByVhapePolicyIndex = "vhape.io/vpa-by-vhape-policy"
)

// AddDeploymentToVPAsIndex adds a Deployment-to-VPA index to the VPA informer.
func AddDeploymentToVPAsIndex(informer vpainformers.VerticalPodAutoscalerInformer) error {
	if informer == nil {
		return fmt.Errorf("vpa informer is nil")
	}

	return informer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		VPAByDeploymentIndex:  GetAssociatedVPADeploymentKey,
		VPAByVhapePolicyIndex: GetAssociatedVPAVhapePolicyKey,
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

// GetAssociatedVPAVhapePolicyKey returns the name of the VhapePolicy selected
// by a VPA. VPAs without the policy annotation are intentionally not indexed.
func GetAssociatedVPAVhapePolicyKey(obj interface{}) ([]string, error) {
	vpa, ok := obj.(*vpav1.VerticalPodAutoscaler)
	if !ok || vpa == nil {
		return nil, nil
	}

	policyName := vpa.Annotations[vhapev1alpha1.VhapePolicyAnnotation]
	if policyName == "" {
		return nil, nil
	}

	return []string{policyName}, nil
}

func NamespacedKey(namespace, name string) string {
	return namespace + "/" + name
}
