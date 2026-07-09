package scope

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vhapelisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.vhape.io/v1alpha1"
)

const (
	ReasonWatched             = "watched"
	ReasonNamespaceNotWatched = "namespace-not-watched"
	ReasonWorkloadIgnored     = "workload-ignored"

	deploymentAPIVersion = "apps/v1"
	deploymentKind       = "Deployment"
)

// Decision describes whether VHAPE Watcher should manage a workload.
type Decision struct {
	ShouldManage bool
	Reason       string
}

// Scope decides whether a Deployment is inside VHAPE Watcher's management scope.
//
// It reads from informer-backed listers. These listers read local caches and do
// not call the Kubernetes API Server directly.
type Scope struct {
	watchedNamespaceLister vhapelisters.VhapeWatchedNamespaceLister
	ignoredWorkloadLister vhapelisters.VhapeIgnoredWorkloadLister
}

// New creates a Scope resolver.
func New(
	watchedNamespaceLister vhapelisters.VhapeWatchedNamespaceLister,
	ignoredWorkloadLister vhapelisters.VhapeIgnoredWorkloadLister,
) (*Scope, error) {
	if watchedNamespaceLister == nil {
		return nil, fmt.Errorf("vhape watched namespace lister is nil")
	}
	if ignoredWorkloadLister == nil {
		return nil, fmt.Errorf("vhape ignored workload lister is nil")
	}

	return &Scope{
		watchedNamespaceLister: watchedNamespaceLister,
		ignoredWorkloadLister: ignoredWorkloadLister,
	}, nil
}

// ShouldManageDeployment returns whether the Deployment should be managed by
// VHAPE Watcher.
//
// Rules:
// - namespace must have a VhapeWatchedNamespace
// - Deployment must not be targeted by a VhapeIgnoredWorkload
func (s *Scope) ShouldManageDeployment(dep *appsv1.Deployment) (Decision, error) {
	if dep == nil {
		return Decision{}, fmt.Errorf("deployment is nil")
	}

	watched, err := s.IsNamespaceWatched(dep.Namespace)
	if err != nil {
		return Decision{}, err
	}
	if !watched {
		return Decision{
			ShouldManage: false,
			Reason:       ReasonNamespaceNotWatched,
		}, nil
	}

	ignored, err := s.IsDeploymentIgnored(dep)
	if err != nil {
		return Decision{}, err
	}
	if ignored {
		return Decision{
			ShouldManage: false,
			Reason:       ReasonWorkloadIgnored,
		}, nil
	}

	return Decision{
		ShouldManage: true,
		Reason:       ReasonWatched,
	}, nil
}

// IsNamespaceWatched returns true when a VhapeWatchedNamespace exists with
// metadata.name equal to the namespace name.
func (s *Scope) IsNamespaceWatched(namespace string) (bool, error) {
	if namespace == "" {
		return false, fmt.Errorf("namespace is empty")
	}

	_, err := s.watchedNamespaceLister.Get(namespace)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get VhapeWatchedNamespace %q from cache: %w", namespace, err)
	}

	return true, nil
}

// IsDeploymentIgnored returns true when a VhapeIgnoredWorkload targets the given Deployment.
func (s *Scope) IsDeploymentIgnored(dep *appsv1.Deployment) (bool, error) {
	if dep == nil {
		return false, fmt.Errorf("deployment is nil")
	}

	ignoredWorkloads, err := s.ignoredWorkloadLister.List(labels.Everything())
	if err != nil {
		return false, fmt.Errorf("list VhapeIgnoredWorkloads from cache: %w", err)
	}

	for _, ignored := range ignoredWorkloads {
		if ignored == nil {
			continue
		}

		if targetsDeployment(ignored.Spec.TargetRef, dep) {
			return true, nil
		}
	}

	return false, nil
}

func targetsDeployment(ref vhapev1alpha1.TargetRef, dep *appsv1.Deployment) bool {
	return ref.APIVersion == deploymentAPIVersion &&
		strings.EqualFold(ref.Kind, deploymentKind) &&
		ref.Namespace == dep.Namespace &&
		ref.Name == dep.Name
}