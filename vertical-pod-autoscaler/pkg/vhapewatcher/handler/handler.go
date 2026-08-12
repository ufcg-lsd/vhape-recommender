package handler

import (
	"fmt"

	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"

	"k8s.io/client-go/tools/cache"
)

// DeploymentSink receives deployment reconciliation requests produced by handlers.
// The reconciler should implement this interface.
type DeploymentSink interface {
	EnqueueDeployment(namespace, name string)
	EnqueueDeploymentsInNamespace(namespace string)
	EnqueueDeploymentsMatchingNamespaceRegex(regexCode string)
}

// Handler registers event handlers on informers.
type Handler struct {
	sink DeploymentSink
}

// New creates a Handler instance.
func New(sink DeploymentSink) (*Handler, error) {
	if sink == nil {
		return nil, fmt.Errorf("deployment sink is nil")
	}

	return &Handler{sink: sink}, nil
}

// eventHandlerReceiver is the subset of cache.SharedIndexInformer used to attach handlers.
// It keeps handler registration testable without starting real informers.
type eventHandlerReceiver interface {
	AddEventHandler(cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error)
}

// sharedInformerProvider is implemented by generated typed informer interfaces.
type sharedInformerProvider interface {
	Informer() cache.SharedIndexInformer
}

type handlerRegistration struct {
	name     string
	receiver eventHandlerReceiver
	handler  cache.ResourceEventHandler
}

// RegisterHandlerFunctionsOnInformers connects each informer to its resource-specific handlers.
func (h *Handler) RegisterHandlerFunctionsOnInformers(informerSet *watcherinformers.Informers) error {
	if informerSet == nil {
		return fmt.Errorf("informers is nil")
	}

	return registerHandlers([]handlerRegistration{
		{
			name:     "deployment",
			receiver: informerReceiver(informerSet.Deployment),
			handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    h.onDeploymentAdd,
				UpdateFunc: h.onDeploymentUpdate,
				DeleteFunc: h.onDeploymentDelete,
			},
		},
		{
			name:     "vpa",
			receiver: informerReceiver(informerSet.VPA),
			handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    h.onVPAAdd,
				UpdateFunc: h.onVPAUpdate,
				DeleteFunc: h.onVPADelete,
			},
		},
		{
			name:     "vhape watched namespace",
			receiver: informerReceiver(informerSet.VhapeWatchedNamespace),
			handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    h.onWatchedNamespaceAdd,
				UpdateFunc: h.onWatchedNamespaceUpdate,
				DeleteFunc: h.onWatchedNamespaceDelete,
			},
		},
		{
			name:     "vhape watched namespace regex",
			receiver: informerReceiver(informerSet.VhapeWatchedNamespaceRegex),
			handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    h.onWatchedNamespaceRegexAdd,
				UpdateFunc: h.onWatchedNamespaceRegexUpdate,
				DeleteFunc: h.onWatchedNamespaceRegexDelete,
			},
		},
		{
			name:     "vhape ignored namespace",
			receiver: informerReceiver(informerSet.VhapeIgnoredNamespace),
			handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    h.onIgnoredNamespaceAdd,
				UpdateFunc: h.onIgnoredNamespaceUpdate,
				DeleteFunc: h.onIgnoredNamespaceDelete,
			},
		},
		{
			name:     "vhape ignored workload",
			receiver: informerReceiver(informerSet.VhapeIgnoredWorkload),
			handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    h.onIgnoredWorkloadAdd,
				UpdateFunc: h.onIgnoredWorkloadUpdate,
				DeleteFunc: h.onIgnoredWorkloadDelete,
			},
		},
	})
}

func informerReceiver(informer sharedInformerProvider) eventHandlerReceiver {
	if informer == nil {
		return nil
	}
	return informer.Informer()
}

// registerHandlers attaches each resource-specific handler to its informer.
func registerHandlers(registrations []handlerRegistration) error {
	for _, registration := range registrations {
		if registration.receiver == nil {
			return fmt.Errorf("%s informer is nil", registration.name)
		}

		if _, err := registration.receiver.AddEventHandler(registration.handler); err != nil {
			return fmt.Errorf("register %s handlers: %w", registration.name, err)
		}
	}

	return nil
}
