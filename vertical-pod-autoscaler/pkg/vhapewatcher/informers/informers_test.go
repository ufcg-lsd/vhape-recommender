package informers

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vhapefake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	vhapeclient "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/client"
)

func TestNew(t *testing.T) {
	clients := newTestClients()

	informers, err := New(clients)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	if informers == nil {
		t.Fatal("New() returned nil informers")
	}
	if informers.Deployment == nil {
		t.Fatal("Deployment informer is nil")
	}
	if informers.Namespace == nil {
		t.Fatal("Namespace informer is nil")
	}
	if informers.VPA == nil {
		t.Fatal("VPA informer is nil")
	}
	if informers.VhapeWatchedNamespace == nil {
		t.Fatal("VhapeWatchedNamespace informer is nil")
	}
	if informers.VhapeWatchedNamespaceRegex == nil {
		t.Fatal("VhapeWatchedNamespaceRegex informer is nil")
	}
	if informers.VhapeIgnoredNamespace == nil {
		t.Fatal("VhapeIgnoredNamespace informer is nil")
	}
	if informers.VhapeIgnoredWorkload == nil {
		t.Fatal("VhapeIgnoredWorkload informer is nil")
	}
}

func TestNewRejectsInvalidClients(t *testing.T) {
	tests := []struct {
		name    string
		clients *vhapeclient.Clients
	}{
		{
			name:    "nil clients",
			clients: nil,
		},
		{
			name: "nil Kubernetes client",
			clients: &vhapeclient.Clients{
				Vhape: vhapefake.NewSimpleClientset(),
			},
		},
		{
			name: "nil VHAPE client",
			clients: &vhapeclient.Clients{
				Kube: kubefake.NewSimpleClientset(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.clients); err == nil {
				t.Fatal("New() expected error, got nil")
			}
		})
	}
}

func TestNewRegistersDeploymentIndexes(t *testing.T) {
	const (
		namespace           = "test-namespace"
		deploymentName      = "test-deployment"
		vpaName             = "test-vpa"
		ignoredWorkloadName = "test-ignored-workload"
	)

	informers, err := New(newTestClients())
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	vpa := &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: vpaName, Namespace: namespace},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
				Name:       deploymentName,
			},
		},
	}
	if err := informers.VPA.Informer().GetIndexer().Add(vpa); err != nil {
		t.Fatalf("add VPA to indexer: %v", err)
	}

	ignoredWorkload := &vhapev1alpha1.VhapeIgnoredWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: ignoredWorkloadName},
		Spec: vhapev1alpha1.VhapeIgnoredWorkloadSpec{
			TargetRef: corev1.ObjectReference{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
				Namespace:  namespace,
				Name:       deploymentName,
			},
		},
	}
	if err := informers.VhapeIgnoredWorkload.Informer().GetIndexer().Add(ignoredWorkload); err != nil {
		t.Fatalf("add VhapeIgnoredWorkload to indexer: %v", err)
	}

	key := NamespacedKey(namespace, deploymentName)

	vpas, err := informers.VPA.Informer().GetIndexer().ByIndex(VPAByDeploymentIndex, key)
	if err != nil {
		t.Fatalf("list VPAs by index: %v", err)
	}
	if len(vpas) != 1 || vpas[0] != vpa {
		t.Fatalf("indexed VPAs = %#v, want [%p]", vpas, vpa)
	}

	ignoredWorkloads, err := informers.VhapeIgnoredWorkload.Informer().GetIndexer().ByIndex(IgnoredWorkloadByDeploymentIndex, key)
	if err != nil {
		t.Fatalf("list VhapeIgnoredWorkloads by index: %v", err)
	}
	if len(ignoredWorkloads) != 1 || ignoredWorkloads[0] != ignoredWorkload {
		t.Fatalf("indexed VhapeIgnoredWorkloads = %#v, want [%p]", ignoredWorkloads, ignoredWorkload)
	}
}

func newTestClients() *vhapeclient.Clients {
	return &vhapeclient.Clients{
		Kube:  kubefake.NewSimpleClientset(),
		Vhape: vhapefake.NewSimpleClientset(),
	}
}
