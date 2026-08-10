package testutil

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
)

func NewDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-" + namespace + "-" + name),
		},
	}
}

func NewVPA(name, namespace, targetName string) *vpav1.VerticalPodAutoscaler {
	return NewVPAWithTarget(
		name,
		namespace,
		appsv1.SchemeGroupVersion.String(),
		"Deployment",
		targetName,
	)
}

func NewVPAWithTarget(name, namespace, targetAPIVersion, targetKind, targetName string) *vpav1.VerticalPodAutoscaler {
	return &vpav1.VerticalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vpav1.SchemeGroupVersion.String(),
			Kind:       "VerticalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: targetAPIVersion,
				Kind:       targetKind,
				Name:       targetName,
			},
		},
	}
}

func NewWatchedNamespace(name string) *vhapev1alpha1.VhapeWatchedNamespace {
	return NewWatchedNamespaceWithPolicy(name, TestPolicyNamespace, TestPolicyName, TestVPAUpdateMode)
}

func NewWatchedNamespaceWithPolicy(
	namespace string,
	policyNamespace string,
	policyName string,
	updateMode vpav1.UpdateMode,
) *vhapev1alpha1.VhapeWatchedNamespace {
	return &vhapev1alpha1.VhapeWatchedNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
		Spec: vhapev1alpha1.VhapeWatchedNamespaceSpec{
			VhapePolicyRef: vhapev1alpha1.VhapePolicyRef{
				Namespace: policyNamespace,
				Name:      policyName,
			},
			VPAUpdateMode: updateMode,
		},
	}
}

func NewIgnoredWorkload(name, targetNamespace, targetName string) *vhapev1alpha1.VhapeIgnoredWorkload {
	return NewIgnoredWorkloadWithTarget(
		name,
		appsv1.SchemeGroupVersion.String(),
		"Deployment",
		targetNamespace,
		targetName,
	)
}

func NewIgnoredWorkloadWithTarget(
	name string,
	targetAPIVersion string,
	targetKind string,
	targetNamespace string,
	targetName string,
) *vhapev1alpha1.VhapeIgnoredWorkload {
	return &vhapev1alpha1.VhapeIgnoredWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: vhapev1alpha1.VhapeIgnoredWorkloadSpec{
			TargetRef: NewTargetRef(targetAPIVersion, targetKind, targetNamespace, targetName),
			Reason:    "test",
		},
	}
}

func NewTargetRef(apiVersion, kind, namespace, name string) corev1.ObjectReference {
	return corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}
}
