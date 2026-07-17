package handler

import (
	"testing"

)

const (
	testNamespace      = "producao"
	testDeploymentName = "api"
)

type fakeDeploymentSink struct {
	deployments []string
	namespaces  []string
}

func (s *fakeDeploymentSink) EnqueueDeployment(namespace, name string) {
	s.deployments = append(s.deployments, namespace+"/"+name)
}

func (s *fakeDeploymentSink) EnqueueDeploymentsInNamespace(namespace string) {
	s.namespaces = append(s.namespaces, namespace)
}

func TestNewHandler(t *testing.T) {
	t.Run("returns handler with valid sink", func(t *testing.T) {
		handler, err := New(&fakeDeploymentSink{})
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if handler == nil {
			t.Fatal("New() returned nil handler")
		}
	})

	t.Run("rejects nil sink", func(t *testing.T) {
		handler, err := New(nil)
		if err == nil {
			t.Fatal("New() expected error, got nil")
		}
		if handler != nil {
			t.Fatalf("New() returned handler = %#v, want nil", handler)
		}
	})
}
