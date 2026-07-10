package vpaservice

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	vpaInformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
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

func (s *VPAService) EnsureNoGeneratedVPAForDeployment(ctx context.Context, vpas []*vpav1.VerticalPodAutoscaler, reason string) error {
	deleted := 0
	for _, vpa := range vpas {
		if IsManagedByWatcher(vpa) {
			if err := s.deleteVPA(ctx, vpa, reason); err != nil {
				return err
			}
			deleted++
		}
	}

	if deleted == 0 {
		klog.V(4).InfoS("No generated VPA to delete", "reason", reason)
	}

	return nil
}

func (s *VPAService) ListForDeployment(dep *appsv1.Deployment) ([]*vpav1.VerticalPodAutoscaler, error) {
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}

	items, err := s.informer.
		Informer().
		GetIndexer().
		ByIndex(VPAByDeploymentIndexName, namespacedKey(dep.Namespace, dep.Name))

	if err != nil {
		return nil, fmt.Errorf("list VPAs indexed by Deployment %q/%q: %w", dep.Namespace, dep.Name, err)
	}

	vpas := make([]*vpav1.VerticalPodAutoscaler, 0, len(items))
	for _, item := range items {
		vpa, ok := item.(*vpav1.VerticalPodAutoscaler)
		if !ok {
			klog.V(4).InfoS("Ignoring non-VPA object returned by VPA index", "deployment", klog.KObj(dep))
			continue
		}

		vpas = append(vpas, vpa)
	}

	klog.V(5).InfoS("Listed VPAs for Deployment", "deployment", klog.KObj(dep), "count", len(vpas))
	return vpas, nil
}

func (s *VPAService) CreateGeneratedVPAForDeployment(ctx context.Context, dep *appsv1.Deployment) error {
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

	klog.InfoS("Created generated VPA for Deployment", "deployment", klog.KObj(dep), "vpa", klog.KObj(vpa))
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
		klog.V(4).InfoS("Generated VPA was already deleted", "vpa", klog.KObj(vpa), "reason", reason)
		return nil
	}

	if err != nil {
		return fmt.Errorf("delete VPA %q/%q: %w", vpa.Namespace, vpa.Name, err)
	}

	klog.InfoS("Deleted generated VPA", "vpa", klog.KObj(vpa), "reason", reason)
	return nil
}

func IsManagedByWatcher(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa == nil {
		return false
	}

	return vpa.Labels[ManagedByLabel] == ManagedByValue
}
