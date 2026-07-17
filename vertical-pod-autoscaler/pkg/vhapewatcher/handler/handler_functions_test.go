package handler

import (
	"testing"

	"k8s.io/client-go/tools/cache"
)

func TestDeploymentHandlers(t *testing.T) {
	t.Run("add enqueues deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentAdd(newDeployment(testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
		assertNamespaces(t, sink, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentAdd("not-a-deployment")

		assertDeployments(t, sink, nil)
		assertNamespaces(t, sink, nil)
	})

	t.Run("update is ignored", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentUpdate(
			newDeployment(testNamespace, "old"),
			newDeployment(testNamespace, "new"),
		)

		assertDeployments(t, sink, nil)
		assertNamespaces(t, sink, nil)
	})

	t.Run("delete is ignored", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentDelete(newDeployment(testNamespace, testDeploymentName))

		assertDeployments(t, sink, nil)
		assertNamespaces(t, sink, nil)
	})
}

func TestVPAHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAAdd(newVPA("vpa", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAAdd("not-a-vpa")

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores VPA without targetRef", func(t *testing.T) {
		handler, sink := newHandler(t)

		vpa := newVPA("vpa", testNamespace, testDeploymentName)
		vpa.Spec.TargetRef = nil
		handler.onVPAAdd(vpa)

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores non Deployment target", func(t *testing.T) {
		handler, sink := newHandler(t)

		vpa := newVPA("vpa", testNamespace, testDeploymentName)
		vpa.Spec.TargetRef.Kind = "StatefulSet"
		handler.onVPAAdd(vpa)

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores target with empty name", func(t *testing.T) {
		handler, sink := newHandler(t)

		vpa := newVPA("vpa", testNamespace, "")
		handler.onVPAAdd(vpa)

		assertDeployments(t, sink, nil)
	})

	t.Run("update enqueues old and new targets", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAUpdate(
			newVPA("vpa", testNamespace, "old-api"),
			newVPA("vpa", testNamespace, "new-api"),
		)

		assertDeployments(t, sink, []string{"producao/old-api", "producao/new-api"})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAUpdate("not-a-vpa", newVPA("vpa", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(newVPA("vpa", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(cache.DeletedFinalStateUnknown{
			Obj: newVPA("vpa", testNamespace, testDeploymentName),
		})

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(cache.DeletedFinalStateUnknown{Obj: "not-a-vpa"})

		assertDeployments(t, sink, nil)
	})
}

func TestWatchedNamespaceHandlers(t *testing.T) {
	t.Run("add enqueues all deployments in namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceAdd(newWatchedNamespace(testNamespace))

		assertNamespaces(t, sink, []string{"producao"})
		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceAdd("not-a-watched-namespace")

		assertNamespaces(t, sink, nil)
		assertDeployments(t, sink, nil)
	})

	t.Run("update enqueues all deployments in new namespace object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceUpdate(
			newWatchedNamespace("old"),
			newWatchedNamespace(testNamespace),
		)

		assertNamespaces(t, sink, []string{"producao"})
	})

	t.Run("update ignores unexpected new object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceUpdate(newWatchedNamespace("old"), "not-a-watched-namespace")

		assertNamespaces(t, sink, nil)
	})

	t.Run("delete enqueues all deployments in namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(newWatchedNamespace(testNamespace))

		assertNamespaces(t, sink, []string{"producao"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{
			Obj: newWatchedNamespace(testNamespace),
		})

		assertNamespaces(t, sink, []string{"producao"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{Obj: "not-a-watched-namespace"})

		assertNamespaces(t, sink, nil)
	})
}

func TestIgnoredWorkloadHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd(newIgnoredWorkload("ignored", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd("not-an-ignored-workload")

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores missing target namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd(newIgnoredWorkload("ignored", "", testDeploymentName))

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores missing target name", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd(newIgnoredWorkload("ignored", testNamespace, ""))

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores non Deployment target", func(t *testing.T) {
		handler, sink := newHandler(t)

		ignored := newIgnoredWorkload("ignored", testNamespace, testDeploymentName)
		ignored.Spec.TargetRef.Kind = "StatefulSet"
		handler.onIgnoredWorkloadAdd(ignored)

		assertDeployments(t, sink, nil)
	})

	t.Run("update enqueues old and new targets", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadUpdate(
			newIgnoredWorkload("ignored-old", testNamespace, "old-api"),
			newIgnoredWorkload("ignored-new", testNamespace, "new-api"),
		)

		assertDeployments(t, sink, []string{"producao/old-api", "producao/new-api"})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadUpdate("not-an-ignored-workload", newIgnoredWorkload("ignored", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(newIgnoredWorkload("ignored", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{
			Obj: newIgnoredWorkload("ignored", testNamespace, testDeploymentName),
		})

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{Obj: "not-an-ignored-workload"})

		assertDeployments(t, sink, nil)
	})
}
