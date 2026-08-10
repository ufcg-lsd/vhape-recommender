package scope

import (
	"fmt"
	"regexp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
)

const (
	ReasonWatched          = "watched"
	ReasonRegexWatched     = "regex-watched"
	ReasonNotWatched       = "not-watched"
	ReasonWorkloadIgnored  = "workload-ignored"
	ReasonNamespaceIgnored = "namespace-ignored"
)

// Decision describes whether VHAPE Watcher should manage a workload.
type Decision struct {
	ShouldManage  bool
	Reason        string
	DesiredConfig vhapev1alpha1.VhapeWatchedNamespaceSpec
}

// Scope decides whether a Deployment is inside VHAPE Watcher's management scope.
//
// It reads from informer-backed caches and does not call the Kubernetes API
// Server directly.
type Scope struct {
	informers *watcherinformers.Informers
}

// New creates a Scope resolver from informer-backed caches.
func New(informerSet *watcherinformers.Informers) (*Scope, error) {
	if informerSet == nil {
		return nil, fmt.Errorf("informers is nil")
	}
	if informerSet.Namespace == nil {
		return nil, fmt.Errorf("namespace informer is nil")
	}
	if informerSet.VhapeWatchedNamespace == nil {
		return nil, fmt.Errorf("vhape watched namespace informer is nil")
	}
	if informerSet.VhapeWatchedNamespaceRegex == nil {
		return nil, fmt.Errorf("vhape watched namespace regex informer is nil")
	}
	if informerSet.VhapeIgnoredNamespace == nil {
		return nil, fmt.Errorf("vhape ignored namespace informer is nil")
	}
	if informerSet.VhapeIgnoredWorkload == nil {
		return nil, fmt.Errorf("vhape ignored workload informer is nil")
	}

	return &Scope{informers: informerSet}, nil
}

// GetNamespacesMatchingRegex returns namespaces currently present in the Namespace
// informer cache whose names match regexCode.
func (s *Scope) GetNamespacesMatchingRegex(regexCode string) ([]*corev1.Namespace, error) {
	if regexCode == "" {
		return nil, fmt.Errorf("namespace regex is empty")
	}

	compiledRegex, err := regexp.Compile(regexCode)
	if err != nil {
		return nil, fmt.Errorf("compile namespace regex %q: %w", regexCode, err)
	}

	namespaces, err := s.informers.Namespace.Lister().List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list Namespaces from cache: %w", err)
	}

	matchingNamespaces := make([]*corev1.Namespace, 0)
	for _, namespace := range namespaces {
		if namespace == nil || !compiledRegex.MatchString(namespace.Name) {
			continue
		}

		matchingNamespaces = append(matchingNamespaces, namespace)
	}

	return matchingNamespaces, nil
}

// GetWatchedNamespaceRegexesMatchingNamespace returns VhapeWatchedNamespaceRegex
// resources currently present in the informer cache whose regex matches namespace.
func (s *Scope) GetWatchedNamespaceRegexesMatchingNamespace(namespace string) ([]*vhapev1alpha1.VhapeWatchedNamespaceRegex, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is empty")
	}

	watchedNamespaceRegexes, err := s.informers.VhapeWatchedNamespaceRegex.Lister().List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list VhapeWatchedNamespaceRegexes from cache: %w", err)
	}

	matchingRegexes := make([]*vhapev1alpha1.VhapeWatchedNamespaceRegex, 0)
	for _, watchedNamespaceRegex := range watchedNamespaceRegexes {
		if watchedNamespaceRegex == nil {
			continue
		}

		compiledRegex, err := regexp.Compile(watchedNamespaceRegex.Spec.Regex)
		if err != nil {
			return nil, fmt.Errorf(
				"compile namespace regex %q from VhapeWatchedNamespaceRegex %q: %w",
				watchedNamespaceRegex.Spec.Regex,
				watchedNamespaceRegex.Name,
				err,
			)
		}

		if compiledRegex.MatchString(namespace) {
			matchingRegexes = append(matchingRegexes, watchedNamespaceRegex)
		}
	}

	return matchingRegexes, nil
}

// ShouldManageDeployment returns whether the Deployment should be managed by
// VHAPE Watcher and the configuration that should be applied.
//
// Rules, in order of precedence:
// - Deployment must not be targeted by a VhapeIgnoredWorkload
// - namespace must not be marked by a VhapeIgnoredNamespace
// - a VhapeWatchedNamespace provides the namespace-specific configuration
// - otherwise, the oldest matching VhapeWatchedNamespaceRegex provides the configuration
// - Deployment is not managed when no watched namespace configuration matches
func (s *Scope) ShouldManageDeployment(dep *appsv1.Deployment) (Decision, error) {
	if dep == nil {
		return Decision{}, fmt.Errorf("deployment is nil")
	}

	// Check if deployment is ignored.
	ignoredWorkload, err := s.GetIgnoredWorkload(dep)
	if err != nil {
		return Decision{}, err
	}

	if ignoredWorkload != nil {
		return Decision{
			ShouldManage: false,
			Reason:       ReasonWorkloadIgnored,
		}, nil
	}

	// Check if namespace is ignored.
	ignoredNamespace, err := s.GetIgnoredNamespace(dep.Namespace)
	if err != nil {
		return Decision{}, err
	}

	if ignoredNamespace != nil {
		return Decision{
			ShouldManage: false,
			Reason:       ReasonNamespaceIgnored,
		}, nil
	}

	// Checks if namespace has specific configuration
	watchedNamespace, err := s.GetWatchedNamespace(dep.Namespace)
	if err != nil {
		return Decision{}, err
	}

	if watchedNamespace != nil {
		return Decision{
			ShouldManage:  true,
			Reason:        ReasonWatched,
			DesiredConfig: watchedNamespace.Spec,
		}, nil
	}

	// Checks if namespace has a regex-selected configuration.
	// When multiple regex resources match, the oldest one wins.
	regexes, err := s.GetWatchedNamespaceRegexesMatchingNamespace(dep.Namespace)
	if err != nil {
		return Decision{}, err
	}

	if oldestRegex := oldestWatchedNamespaceRegex(regexes); oldestRegex != nil {
		return Decision{
			ShouldManage:  true,
			Reason:        ReasonRegexWatched,
			DesiredConfig: oldestRegex.Spec.VhapeWatchedNamespaceSpec,
		}, nil
	}

	return Decision{
		ShouldManage: false,
		Reason:       ReasonNotWatched,
	}, nil
}

// GetWatchedNamespace returns a VhapeWatchedNamespace.
func (s *Scope) GetWatchedNamespace(namespace string) (*vhapev1alpha1.VhapeWatchedNamespace, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is empty")
	}

	watchedNamespace, err := s.informers.VhapeWatchedNamespace.Lister().Get(namespace)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get VhapeWatchedNamespace %q from cache: %w", namespace, err)
	}

	return watchedNamespace, nil
}

// GetIgnoredNamespace returns the VhapeIgnoredNamespace for the given namespace,
// if one exists in the informer cache.
func (s *Scope) GetIgnoredNamespace(namespace string) (*vhapev1alpha1.VhapeIgnoredNamespace, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is empty")
	}

	ignoredNamespace, err := s.informers.VhapeIgnoredNamespace.Lister().Get(namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("get VhapeIgnoredNamespace %q from cache: %w", namespace, err)
	}

	return ignoredNamespace, nil
}

// GetIgnoredWorkload returns a VhapeIgnoredWorkload targeting the Deployment,
// if one exists in the informer cache.
func (s *Scope) GetIgnoredWorkload(dep *appsv1.Deployment) (*vhapev1alpha1.VhapeIgnoredWorkload, error) {
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}

	items, err := s.informers.VhapeIgnoredWorkload.Informer().GetIndexer().ByIndex(
		watcherinformers.IgnoredWorkloadByDeploymentIndex,
		watcherinformers.NamespacedKey(dep.Namespace, dep.Name),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get VhapeIgnoredWorkload targeting Deployment %q/%q from cache: %w",
			dep.Namespace,
			dep.Name,
			err,
		)
	}

	if len(items) == 0 {
		return nil, nil
	}

	ignoredWorkload, ok := items[0].(*vhapev1alpha1.VhapeIgnoredWorkload)
	if !ok {
		return nil, fmt.Errorf("unexpected object type in VhapeIgnoredWorkload index: %T", items[0])
	}

	return ignoredWorkload, nil
}

func oldestWatchedNamespaceRegex(regexes []*vhapev1alpha1.VhapeWatchedNamespaceRegex) *vhapev1alpha1.VhapeWatchedNamespaceRegex {
	if len(regexes) == 0 {
		return nil
	}

	oldest := regexes[0]

	for _, regex := range regexes[1:] {
		if regex.CreationTimestamp.Before(&oldest.CreationTimestamp) {
			oldest = regex
		}
	}

	return oldest
}
