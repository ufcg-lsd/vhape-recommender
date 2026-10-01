package vpaservice

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "vhape-watcher"

	VhapeLabel = "autoscaling.vhape.io/recommender"

	GeneratedVPANamePrefix = "" // empty for now

	VhapePolicyAnnotation = vhapev1alpha1.VhapePolicyAnnotation

	VhapeRecommenderName = "vhape-recommender"
)

// GenerateVPAForDeployment builds the desired VPA object for a Deployment.
func GenerateVPAForDeployment(name string, dep *appsv1.Deployment, options vhapev1alpha1.VhapeWatchedNamespaceSpec) (*vpav1.VerticalPodAutoscaler, error) {
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}

	if name == "" {
		return nil, fmt.Errorf("name is empty")
	}

	updateMode := options.VPAUpdateMode
	controlledValues := vpav1.ContainerControlledValuesRequestsOnly // Only requests will be updated

	return &vpav1.VerticalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vpav1.SchemeGroupVersion.String(),
			Kind:       "VerticalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       dep.Namespace,
			Labels:          LabelsForVPA(),
			Annotations:     map[string]string{VhapePolicyAnnotation: options.VhapePolicyName},
			OwnerReferences: OwnerReferencesForDeployment(dep),
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
				Name:       dep.Name,
			},
			Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
				{
					Name: VhapeRecommenderName,
				},
			},
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &updateMode,
			},
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{
						ContainerName:    vpav1.DefaultContainerResourcePolicy, // * all containers from deployment
						ControlledValues: &controlledValues,
					},
				},
			},
		},
	}, nil
}

// NameForDeployment returns the deterministic name VHAPE Watcher uses for the generated VPA associated with a Deployment.
func NameForDeployment(dep *appsv1.Deployment) string {
	name := GeneratedVPANamePrefix + dep.Name
	if len(name) > 253 {
		name = name[:253]
	}

	return name
}

func LabelsForVPA() map[string]string {
	return map[string]string{
		ManagedByLabel: ManagedByValue,
		VhapeLabel:     VhapeRecommenderName,
	}
}

// HasVhapeRecommenderLabel reports whether the VPA identifies a VHAPE
// recommender with a non-empty label value.
func HasVhapeRecommenderLabel(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa == nil {
		return false
	}

	return vpa.Labels[VhapeLabel] != ""
}

// VhapePolicyName returns the VhapePolicy selected by the VPA, if any.
func VhapePolicyName(vpa *vpav1.VerticalPodAutoscaler) string {
	if vpa == nil {
		return ""
	}

	return vpa.Annotations[VhapePolicyAnnotation]
}

// LatestVhapeVPA returns the most recently created VPA that carries both the
// VHAPE recommender label and policy annotation. Name is used as a stable
// tiebreaker when creation timestamps are equal.
func LatestVhapeVPA(vpas []*vpav1.VerticalPodAutoscaler) *vpav1.VerticalPodAutoscaler {
	var latest *vpav1.VerticalPodAutoscaler
	for _, vpa := range vpas {
		if !HasVhapeRecommenderLabel(vpa) || VhapePolicyName(vpa) == "" {
			continue
		}

		if latest == nil ||
			latest.CreationTimestamp.Time.Before(vpa.CreationTimestamp.Time) ||
			(latest.CreationTimestamp.Equal(&vpa.CreationTimestamp) && latest.Name < vpa.Name) {
			latest = vpa
		}
	}

	return latest
}

func OwnerReferencesForDeployment(dep *appsv1.Deployment) []metav1.OwnerReference {
	controller := true
	return []metav1.OwnerReference{
		{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
			Name:       dep.Name,
			UID:        dep.UID,
			Controller: &controller,
		},
	}
}
