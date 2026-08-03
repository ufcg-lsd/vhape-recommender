package handler

import (
	"testing"

	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	"k8s.io/client-go/tools/cache"
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

func TestDeploymentHandlers(t *testing.T) {
	t.Run("add enqueues deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentAdd(testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentAdd("not-a-deployment")

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("update is ignored", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentUpdate(
			testutil.NewDeployment(testutil.TestNamespace, "old"),
			testutil.NewDeployment(testutil.TestNamespace, "new"),
		)

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("delete is ignored", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentDelete(testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})
}

func TestVPAHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAAdd(testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAAdd("not-a-vpa")

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores VPA without targetRef", func(t *testing.T) {
		handler, sink := newHandler(t)

		vpa := testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName)
		vpa.Spec.TargetRef = nil
		handler.onVPAAdd(vpa)

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores non Deployment target", func(t *testing.T) {
		handler, sink := newHandler(t)

		vpa := testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName)
		vpa.Spec.TargetRef.Kind = "StatefulSet"
		handler.onVPAAdd(vpa)

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores target with empty name", func(t *testing.T) {
		handler, sink := newHandler(t)

		vpa := testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, "")
		handler.onVPAAdd(vpa)

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("update enqueues old and new targets", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAUpdate(
			testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, "old-api"),
			testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, "new-api"),
		)

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/old-api", "producao/new-api"})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAUpdate("not-a-vpa", testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName),
		})

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(cache.DeletedFinalStateUnknown{Obj: "not-a-vpa"})

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})
}

func TestWatchedNamespaceHandlers(t *testing.T) {
	t.Run("add enqueues all deployments in namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceAdd(testutil.NewWatchedNamespace(testutil.TestNamespace))

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{"producao"})
		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceAdd("not-a-watched-namespace")

		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("update enqueues all deployments in new namespace object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceUpdate(
			testutil.NewWatchedNamespace("old"),
			testutil.NewWatchedNamespace(testutil.TestNamespace),
		)

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{"producao"})
	})

	t.Run("update ignores unexpected new object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceUpdate(testutil.NewWatchedNamespace("old"), "not-a-watched-namespace")

		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("delete enqueues all deployments in namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(testutil.NewWatchedNamespace(testutil.TestNamespace))

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{"producao"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewWatchedNamespace(testutil.TestNamespace),
		})

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{"producao"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{Obj: "not-a-watched-namespace"})

		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})
}

func TestIgnoredWorkloadHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd(testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd("not-an-ignored-workload")

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores missing target namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd(testutil.NewIgnoredWorkload("ignored", "", testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores missing target name", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd(testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, ""))

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores non Deployment target", func(t *testing.T) {
		handler, sink := newHandler(t)

		ignored := testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName)
		ignored.Spec.TargetRef.Kind = "StatefulSet"
		handler.onIgnoredWorkloadAdd(ignored)

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("update enqueues old and new targets", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadUpdate(
			testutil.NewIgnoredWorkload("ignored-old", testutil.TestNamespace, "old-api"),
			testutil.NewIgnoredWorkload("ignored-new", testutil.TestNamespace, "new-api"),
		)

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/old-api", "producao/new-api"})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadUpdate("not-an-ignored-workload", testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName),
		})

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{"producao/api"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{Obj: "not-an-ignored-workload"})

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})
}

func newHandler(t *testing.T) (*Handler, *fakeDeploymentSink) {
	t.Helper()

	sink := &fakeDeploymentSink{}
	handler, err := New(sink)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return handler, sink
}
