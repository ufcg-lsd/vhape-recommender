package kube

import (
	"fmt"
	"strings"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpaInformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	"k8s.io/client-go/tools/cache"
)

const VPAByDeploymentIndexName = "vhape.io/vpa-by-deployment"

func AddVPAIndexes(informer vpaInformers.VerticalPodAutoscalerInformer) error {
	if informer == nil {
		return fmt.Errorf("vpa informer is nil")
	}

	return informer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		VPAByDeploymentIndexName: vpaByDeploymentIndex,
	})
}

func vpaByDeploymentIndex(obj interface{}) ([]string, error) {
	vpa, ok := obj.(*vpav1.VerticalPodAutoscaler)
	if !ok {
		return nil, nil
	}
	if vpa.Spec.TargetRef == nil {
		return nil, nil
	}

	ref := vpa.Spec.TargetRef
	if !isDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
		return nil, nil
	}

	return []string{namespacedKey(vpa.Namespace, ref.Name)}, nil
}

func namespacedKey(namespace, name string) string {
	return namespace + "/" + name
}

func isDeploymentTarget(apiVersion, kind, name string) bool {
	return apiVersion == deploymentAPIVersion &&
		strings.EqualFold(kind, deploymentKind) &&
		name != ""
}
