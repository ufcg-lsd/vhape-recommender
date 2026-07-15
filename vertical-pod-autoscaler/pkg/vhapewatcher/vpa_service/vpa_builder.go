package vpaservice

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "vhape-watcher"

	deploymentAPIVersion = "apps/v1"
	deploymentKind       = "Deployment"

	vpaAPIVersion = "autoscaling.k8s.io/v1"
	vpaKind       = "VerticalPodAutoscaler"

	generatedVPANamePrefix = "vhape-generated-"

	vhapePolicyAnnotation = "vhape/policy"
	vhapeRecommenderName  = "vhape-recommender"
)

type GenerationOptions struct {
	VhapePolicyNamespace string
	VhapePolicyName      string
	VPAUpdateMode        vpav1.UpdateMode
}

// GenerateVPAForDeployment builds the desired VPA object for a Deployment.
func (s *VPAService) GenerateVPAForDeployment(dep *appsv1.Deployment, options GenerationOptions) (*vpav1.VerticalPodAutoscaler, error) {
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}

	name := NameForDeployment(dep)

	updateMode := options.VPAUpdateMode
	controlledValues := vpav1.ContainerControlledValuesRequestsOnly

	return &vpav1.VerticalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vpaAPIVersion,
			Kind:       vpaKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       dep.Namespace,
			Labels:          map[string]string{ManagedByLabel: ManagedByValue},
			Annotations:     map[string]string{vhapePolicyAnnotation: PolicyRef(options)},
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
					Name: vhapeRecommenderName,
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

// NameForDeployment returns the deterministic name VHAPE Watcher uses for the generated VPA associated with a Deployment.
func NameForDeployment(dep *appsv1.Deployment) string {
	name := generatedVPANamePrefix + dep.Name
	if len(name) > 253 {
		name = name[:253]
	}

	return name
}

func PolicyRef(options GenerationOptions) string {
	return options.VhapePolicyNamespace + "/" + options.VhapePolicyName
}

func OwnerReferencesForDeployment(dep *appsv1.Deployment) []metav1.OwnerReference {
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
