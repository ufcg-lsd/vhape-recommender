package kube

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpaInformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
)

type VPAServiceConfig struct {
	VhapeRecommenderName        string
	VhapePolicyAnnotation       string
	DefaultVhapePolicyNamespace string
	DefaultVhapePolicyName      string
}

type VPAService struct {
	informer vpaInformers.VerticalPodAutoscalerInformer
	client   vpaclientset.Interface
	config   VPAServiceConfig
}

func NewVPAService(
	informer vpaInformers.VerticalPodAutoscalerInformer,
	client vpaclientset.Interface,
	config VPAServiceConfig,
) (*VPAService, error) {
	if informer == nil {
		return nil, fmt.Errorf("vpa informer is nil")
	}
	if client == nil {
		return nil, fmt.Errorf("vpa client is nil")
	}

	return &VPAService{
		informer: informer,
		client:   client,
		config:   config,
	}, nil
}

func (s *VPAService) EnsureVPAForDeployment(ctx context.Context, dep *appsv1.Deployment) error {
	if dep == nil {
		return fmt.Errorf("deployment is nil")
	}

	vpas, err := s.ListForDeployment(dep)
	if err != nil {
		return err
	}

	if len(vpas) > 1 {
		return nil
	}

	return s.createGeneratedVPA(ctx, dep)
}

func (s *VPAService) EnsureNoGeneratedVPAForDeployment(ctx context.Context, dep *appsv1.Deployment, reason string) error {
	if dep == nil {
		return fmt.Errorf("deployment is nil")
	}

	vpas, err := s.ListForDeployment(dep)
	if err != nil {
		return err
	}

	for _, vpa := range vpas {
		if IsManagedByWatcher(vpa) {
			if err := s.deleteVPA(ctx, vpa, reason); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *VPAService) createGeneratedVPA(ctx context.Context, dep *appsv1.Deployment) error {
	vpa, err := s.GenerateVPAForDeployment(dep)
	if err != nil {
		return err
	}

	_, err = s.client.
		AutoscalingV1().
		VerticalPodAutoscalers(vpa.Namespace).
		Create(ctx, vpa, metav1.CreateOptions{})

	if apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create VPA %q/%q: already exists after preflight check: %w", vpa.Namespace, vpa.Name, err)
	}
	if err != nil {
		return fmt.Errorf("create VPA %q/%q: %w", vpa.Namespace, vpa.Name, err)
	}

	return nil
}

func (s *VPAService) deleteVPA(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler, reason string) error {
	if vpa == nil {
		return nil
	}

	err := s.client.
		AutoscalingV1().
		VerticalPodAutoscalers(vpa.Namespace).
		Delete(ctx, vpa.Name, metav1.DeleteOptions{})

	if apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("delete VPA %q/%q: %w", vpa.Namespace, vpa.Name, err)
	}

	return nil
}

func IsManagedByWatcher(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa == nil {
		return false
	}

	return vpa.Labels[ManagedByLabel] == ManagedByValue
}