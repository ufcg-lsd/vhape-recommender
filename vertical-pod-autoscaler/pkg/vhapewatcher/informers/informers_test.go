package informers

import (
	"testing"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapeclient "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/client"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
	kubefake "k8s.io/client-go/kubernetes/fake"

	vhapefake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
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
	if informers.VPA == nil {
		t.Fatal("VPA informer is nil")
	}
	if informers.VhapeWatchedNamespace == nil {
		t.Fatal("VhapeWatchedNamespace informer is nil")
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

func TestNewRegistersDeploymentToVPAsIndex(t *testing.T) {
	informers, err := New(newTestClients())
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	vpa := testutil.NewVPA(
		testutil.TestVPAName,
		testutil.TestNamespace,
		testutil.TestDeploymentName,
	)
	testutil.AddToIndexer(t, informers.VPA.Informer().GetIndexer(), vpa)

	items, err := informers.VPA.Informer().GetIndexer().ByIndex(
		vpaservice.IndexName,
		testutil.TestNamespace+"/"+testutil.TestDeploymentName,
	)
	if err != nil {
		t.Fatalf("ByIndex() returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("indexed VPAs = %d, want 1", len(items))
	}

	indexedVPA, ok := items[0].(*vpav1.VerticalPodAutoscaler)
	if !ok {
		t.Fatalf("indexed object type = %T, want *VerticalPodAutoscaler", items[0])
	}
	if indexedVPA.Name != testutil.TestVPAName {
		t.Fatalf("indexed VPA name = %q, want %q", indexedVPA.Name, testutil.TestVPAName)
	}
}

func newTestClients() *vhapeclient.Clients {
	return &vhapeclient.Clients{
		Kube:  kubefake.NewSimpleClientset(),
		Vhape: vhapefake.NewSimpleClientset(),
	}
}
