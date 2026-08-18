package vpaservice

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	vpaInformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/autoscaling.k8s.io/v1"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
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
			if err := s.DeleteVPA(ctx, vpa, reason); err != nil {
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
	options vhapev1alpha1.VhapeWatchedNamespaceSpec,
) error {
	if dep == nil {
		return fmt.Errorf("deployment is nil")
	}

	desired, err := GenerateVPAForDeployment(NameForDeployment(dep), dep, options)
	if err != nil {
		return err
	}

	// Find the generated VPA with the desired name.
	var current *vpav1.VerticalPodAutoscaler
	for _, vpa := range vpas {
		if !IsManagedByWatcher(vpa) {
			continue
		}

		if vpa.Namespace == desired.Namespace && vpa.Name == desired.Name {
			current = vpa
			break
		}
	}

	// Ensure the desired VPA exists and is up to date.
	var ensureErr error

	switch {
	case current == nil:
		_, ensureErr = s.ApplyVPA(ctx, desired)

	case IsDesiredGeneratedVPA(current, desired):
		klog.V(4).InfoS(
			"Generated VPA exists and is already up to date",
			"deployment", klog.KObj(dep),
			"vpa", klog.KObj(current),
		)

	default:
		_, ensureErr = s.PatchVPA(ctx, current, desired)
		if ensureErr != nil {
			if deleteErr := s.DeleteVPA(ctx, current, "outdated-vpa"); deleteErr != nil {
				ensureErr = fmt.Errorf(
					"patch generated VPA: %v; delete outdated VPA: %w",
					ensureErr,
					deleteErr,
				)
			}
		}
	}

	// Remove every other generated VPA.
	var cleanupErr error
	for _, vpa := range vpas {
		if !IsManagedByWatcher(vpa) {
			continue
		}

		if vpa.Namespace == desired.Namespace && vpa.Name == desired.Name {
			continue
		}

		if err := s.DeleteVPA(ctx, vpa, "invalid-generated-vpa"); err != nil {
			cleanupErr = err
		}
	}

	if ensureErr != nil {
		return ensureErr
	}

	return cleanupErr
}

func (s *VPAService) ListForDeployment(dep *appsv1.Deployment) ([]*vpav1.VerticalPodAutoscaler, error) {
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}

	items, err := s.informer.
		Informer().
		GetIndexer().
		ByIndex(watcherinformers.VPAByDeploymentIndex, watcherinformers.NamespacedKey(dep.Namespace, dep.Name))

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

func (s *VPAService) ApplyVPA(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler) (*vpav1.VerticalPodAutoscaler, error) {
	if vpa == nil {
		return nil, fmt.Errorf("vpa is nil")
	}

	created, err := s.client.
		AutoscalingV1().
		VerticalPodAutoscalers(vpa.Namespace).
		Create(ctx, vpa, metav1.CreateOptions{})

	if apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create VPA %q/%q: already exists: %w", vpa.Namespace, vpa.Name, err)
	}
	if err != nil {
		return nil, fmt.Errorf("create VPA %q/%q: %w", vpa.Namespace, vpa.Name, err)
	}

	klog.InfoS("Created generated VPA", "vpa", klog.KObj(created))
	return created, nil
}

func (s *VPAService) DeleteVPA(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler, reason string) error {
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

func (s *VPAService) PatchVPA(
	ctx context.Context,
	current *vpav1.VerticalPodAutoscaler,
	desired *vpav1.VerticalPodAutoscaler,
) (*vpav1.VerticalPodAutoscaler, error) {
	if current == nil {
		return nil, fmt.Errorf("current VPA is nil")
	}
	if desired == nil {
		return nil, fmt.Errorf("desired VPA is nil")
	}

	if current.Namespace != desired.Namespace || current.Name != desired.Name {
		return nil, fmt.Errorf(
			"cannot patch VPA %s/%s into %s/%s",
			current.Namespace,
			current.Name,
			desired.Namespace,
			desired.Name,
		)
	}

	modified := current.DeepCopy()

	// Watcher owns the VPA spec.
	modified.Spec = desired.Spec

	// Restore labels owned by the watcher while preserving others.
	if modified.Labels == nil {
		modified.Labels = map[string]string{}
	}
	for key, value := range desired.Labels {
		modified.Labels[key] = value
	}

	// Restore annotations owned by the watcher while preserving others.
	if modified.Annotations == nil {
		modified.Annotations = map[string]string{}
	}
	for key, value := range desired.Annotations {
		modified.Annotations[key] = value
	}

	// Preserve non-controller owner references and restore the desired controller reference.
	modified.OwnerReferences = make([]metav1.OwnerReference, 0, len(current.OwnerReferences)+1)

	for _, ref := range current.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			continue
		}

		modified.OwnerReferences = append(modified.OwnerReferences, ref)
	}

	modified.OwnerReferences = append(
		modified.OwnerReferences,
		desired.OwnerReferences...,
	)

	// create patch json
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}

	modifiedJSON, err := json.Marshal(modified)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.CreateMergePatch(currentJSON, modifiedJSON)
	if err != nil {
		return nil, err
	}

	patched, err := s.client.
		AutoscalingV1().
		VerticalPodAutoscalers(current.Namespace).
		Patch(
			ctx,
			current.Name,
			types.MergePatchType,
			patch,
			metav1.PatchOptions{},
		)
	if err != nil {
		return nil, err
	}

	klog.V(3).InfoS(
		"Patched generated VPA",
		"vpa", klog.KObj(patched),
	)

	return patched, nil
}

func IsDesiredGeneratedVPA(current *vpav1.VerticalPodAutoscaler, desired *vpav1.VerticalPodAutoscaler) bool {
	if current == nil || desired == nil {
		return false
	}

	return current.Namespace == desired.Namespace &&
		current.Name == desired.Name &&
		current.Labels[ManagedByLabel] == desired.Labels[ManagedByLabel] &&
		current.Labels[VhapeLabel] == desired.Labels[VhapeLabel] &&
		current.Annotations[VhapePolicyAnnotation] == desired.Annotations[VhapePolicyAnnotation] &&
		apiequality.Semantic.DeepEqual(
			metav1.GetControllerOf(current),
			metav1.GetControllerOf(desired),
		) &&
		apiequality.Semantic.DeepEqual(current.Spec, desired.Spec)
}

func IsManagedByWatcher(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa == nil {
		return false
	}

	return vpa.Labels[ManagedByLabel] == ManagedByValue
}

func IsDeploymentTarget(apiVersion, kind, name string) bool {
	return apiVersion == appsv1.SchemeGroupVersion.String() &&
		kind == "Deployment" &&
		name != ""
}
