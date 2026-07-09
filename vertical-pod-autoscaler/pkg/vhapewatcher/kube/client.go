package kube

import (
	"fmt"

	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	vpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
)

// Clients groups all Kubernetes clients used by VHAPE Watcher.
type Clients struct {
	// Kube is the native Kubernetes client.
	Kube kubeclient.Interface

	// VPA is the generated clientset from this repository.
	VPA vpaclientset.Interface
}

// NewClients creates all clients needed by VHAPE Watcher.
func NewClients(config *rest.Config) (*Clients, error) {
	if config == nil {
		return nil, fmt.Errorf("kubernetes rest config is nil")
	}

	kubeClient, err := kubeclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	vpaClient, err := vpaclientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create vpa client: %w", err)
	}

	return &Clients{
		Kube: kubeClient,
		VPA:  vpaClient,
	}, nil
}