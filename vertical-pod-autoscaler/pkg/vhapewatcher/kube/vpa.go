package kube

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpalisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.k8s.io/v1"
)

const (
	generatedVPANamePrefix = "vhape-generated-"

	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "vhape-watcher"

	deploymentAPIVersion = "apps/v1"
	deploymentKind       = "Deployment"

	vpaAPIVersion = "autoscaling.k8s.io/v1"
	vpaKind       = "VerticalPodAutoscaler"
)

type VPAParam struct {
	VhapeRecommenderName        string
	VhapePolicyAnnotation       string
	DefaultVhapePolicyNamespace string
	DefaultVhapePolicyName      string
}

// VPANameForDeployment returns the VPA name used when VHAPE Watcher creates a VPA for a Deployment.
func VPANameForDeployment(dep *appsv1.Deployment) (string, error) {
	if dep == nil {
		return "", fmt.Errorf("deployment is nil")
	}
	if dep.Name == "" {
		return "", fmt.Errorf("deployment name is empty")
	}

	return generatedVPANamePrefix + dep.Name, nil
}

// BuildVPAForDeployment builds the VPA object VHAPE Watcher would create for a Deployment.
func BuildVPAForDeployment(dep *appsv1.Deployment, params VPAParam) (*vpav1.VerticalPodAutoscaler, error) {
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}
	if dep.Namespace == "" {
		return nil, fmt.Errorf("deployment namespace is empty")
	}
	if dep.Name == "" {
		return nil, fmt.Errorf("deployment name is empty")
	}
	if dep.UID == "" {
		return nil, fmt.Errorf("deployment uid is empty")
	}
	if params.VhapeRecommenderName == "" {
		return nil, fmt.Errorf("vhape recommender name is empty")
	}
	if params.VhapePolicyAnnotation == "" {
		return nil, fmt.Errorf("vhape policy annotation is empty")
	}
	if params.DefaultVhapePolicyNamespace == "" {
		return nil, fmt.Errorf("default VhapePolicy namespace is empty")
	}
	if params.DefaultVhapePolicyName == "" {
		return nil, fmt.Errorf("default VhapePolicy name is empty")
	}

	name, err := VPANameForDeployment(dep)
	if err != nil {
		return nil, err
	}

	updateMode := vpav1.UpdateModeInPlaceOrRecreate
	controlledValues := vpav1.ContainerControlledValuesRequestsOnly

	return &vpav1.VerticalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vpaAPIVersion,
			Kind:       vpaKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dep.Namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
			Annotations:     DefaultPolicyAnnotations(params),
			OwnerReferences: OwnerReferencesForDeployment(dep),
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: deploymentAPIVersion,
				Kind:       deploymentKind,
				Name:       dep.Name,
			},
			Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
				{
					Name: params.VhapeRecommenderName,
				},
			},
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &updateMode,
			},
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{
						ContainerName:    vpav1.DefaultContainerResourcePolicy,
						ControlledValues: &controlledValues,
					},
				},
			},
		},
	}, nil
}

// DefaultPolicyAnnotations returns the annotations required by VHAPE recommender.
func DefaultPolicyAnnotations(params VPAParam) map[string]string {
	return map[string]string{
		params.VhapePolicyAnnotation: DefaultPolicyRef(params),
	}
}

// DefaultPolicyRef returns the namespace/name reference used by the recommender.
func DefaultPolicyRef(params VPAParam) string {
	return params.DefaultVhapePolicyNamespace + "/" + params.DefaultVhapePolicyName
}

// OwnerReferencesForDeployment returns ownerReferences for a VPA created by VHAPE Watcher.
//
// This allows Kubernetes garbage collection to remove the VPA when the Deployment is deleted.
func OwnerReferencesForDeployment(dep *appsv1.Deployment) []metav1.OwnerReference {
	if dep == nil {
		return nil
	}

	controller := true
	return []metav1.OwnerReference{
		{
			APIVersion: deploymentAPIVersion,
			Kind:       deploymentKind,
			Name:       dep.Name,
			UID:        dep.UID,
			Controller: &controller,
		},
	}
}

// FindVPAForDeployment returns any VPA in the Deployment namespace whose spec.targetRef points to the Deployment.
//
// It uses a lister, so reads come from the informer cache.
func FindVPAForDeployment(
	vpaLister vpalisters.VerticalPodAutoscalerLister,
	dep *appsv1.Deployment,
) (*vpav1.VerticalPodAutoscaler, error) {
	if vpaLister == nil {
		return nil, fmt.Errorf("vpa lister is nil")
	}
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}
	if dep.Namespace == "" {
		return nil, fmt.Errorf("deployment namespace is empty")
	}
	if dep.Name == "" {
		return nil, fmt.Errorf("deployment name is empty")
	}

	vpas, err := vpaLister.VerticalPodAutoscalers(dep.Namespace).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list VPAs in namespace %q from cache: %w", dep.Namespace, err)
	}

	for _, vpa := range vpas {
		if TargetsDeployment(vpa, dep) {
			return vpa, nil
		}
	}

	return nil, nil
}

// TargetsDeployment returns true when the VPA targetRef points to the given Deployment.
func TargetsDeployment(vpa *vpav1.VerticalPodAutoscaler, dep *appsv1.Deployment) bool {
	if vpa == nil || dep == nil || vpa.Spec.TargetRef == nil {
		return false
	}

	ref := vpa.Spec.TargetRef

	return vpa.Namespace == dep.Namespace &&
		ref.APIVersion == deploymentAPIVersion &&
		strings.EqualFold(ref.Kind, deploymentKind) &&
		ref.Name == dep.Name
}
