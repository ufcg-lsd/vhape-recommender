package vpaservice

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	vpaInformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
)

type VPAService struct {
	informer vpaInformers.VerticalPodAutoscalerInformer
	client   vpaclientset.Interface
}

func NewVPAService(
	informer vpaInformers.VerticalPodAutoscalerInformer,
	client vpaclientset.Interface,
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

func (s *VPAService) EnsureOneGeneratedVPAForDeployment(
	ctx context.Context,
	dep *appsv1.Deployment,
	vpas []*vpav1.VerticalPodAutoscaler,
	options GenerationOptions,
) error {
	if dep == nil {
		return fmt.Errorf("deployment is nil")
	}

	desired, err := s.GenerateVPAForDeployment(dep, options)
	if err != nil {
		return err
	}

	// finds if there is an already updated vpa
	var upToDate *vpav1.VerticalPodAutoscaler
	for _, vpa := range vpas {
		if !IsManagedByWatcher(vpa) {
			continue
		}

		if isDesiredGeneratedVPA(vpa, desired) {
			upToDate = vpa
			break
		}
	}

	// ensures only the already existing updated vpa remains
	if upToDate != nil {
		for _, vpa := range vpas {
			if !IsManagedByWatcher(vpa) {
				continue
			}

			if vpa.Namespace == upToDate.Namespace && vpa.Name == upToDate.Name {
				continue
			}

			if err := s.deleteVPA(ctx, vpa, "extra-generated-vpa"); err != nil {
				return err
			}
		}

		klog.V(4).InfoS("Generated VPA exists and is already up to date", "deployment", klog.KObj(dep), "vpa", klog.KObj(upToDate))
		return nil
	}

	// if no existing updated vpa, delete outdated generated vpas
	for _, vpa := range vpas {
		if !IsManagedByWatcher(vpa) {
			continue
		}

		if err := s.deleteVPA(ctx, vpa, "outdated-generated-vpa"); err != nil {
			return err
		}
	}

	// create updated vpa 
	_, err = s.createGeneratedVPA(ctx, dep, desired)
	return err
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

func (s *VPAService) CreateGeneratedVPAForDeployment(
	ctx context.Context,
	dep *appsv1.Deployment,
	options GenerationOptions,
) error {
	vpa, err := s.GenerateVPAForDeployment(dep, options)
	if err != nil {
		return err
	}

	_, err = s.createGeneratedVPA(ctx, dep, vpa)
	return err
}

func (s *VPAService) createGeneratedVPA(
	ctx context.Context,
	dep *appsv1.Deployment,
	vpa *vpav1.VerticalPodAutoscaler,
) (*vpav1.VerticalPodAutoscaler, error) {
	if vpa == nil {
		return nil, fmt.Errorf("vpa is nil")
	}

	created, err := s.client.
		AutoscalingV1().
		VerticalPodAutoscalers(vpa.Namespace).
		Create(ctx, vpa, metav1.CreateOptions{})

	if apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create VPA %q/%q: already exists after cleanup check: %w", vpa.Namespace, vpa.Name, err)
	}
	if err != nil {
		return nil, fmt.Errorf("create VPA %q/%q: %w", vpa.Namespace, vpa.Name, err)
	}

	klog.InfoS("Created generated VPA for Deployment", "deployment", klog.KObj(dep), "vpa", klog.KObj(created))
	return created, nil
}

func isDesiredGeneratedVPA(current *vpav1.VerticalPodAutoscaler, desired *vpav1.VerticalPodAutoscaler) bool {
	if current == nil || desired == nil {
		return false
	}

	return current.Namespace == desired.Namespace &&
		current.Name == desired.Name &&
		apiequality.Semantic.DeepEqual(current.Labels, desired.Labels) &&
		apiequality.Semantic.DeepEqual(current.Annotations, desired.Annotations) &&
		apiequality.Semantic.DeepEqual(current.OwnerReferences, desired.OwnerReferences) &&
		apiequality.Semantic.DeepEqual(current.Spec, desired.Spec)
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
