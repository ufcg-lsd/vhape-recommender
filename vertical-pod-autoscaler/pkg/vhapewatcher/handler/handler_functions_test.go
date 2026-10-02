package handler

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
	"k8s.io/client-go/tools/cache"
)

type fakeDeploymentSink struct {
	deployments []string
	namespaces  []string
	regexes     []string
}

func (s *fakeDeploymentSink) EnqueueDeployment(namespace, name string) {
	s.deployments = append(s.deployments, namespace+"/"+name)
}

func (s *fakeDeploymentSink) EnqueueDeploymentsInNamespace(namespace string) {
	s.namespaces = append(s.namespaces, namespace)
}

func (s *fakeDeploymentSink) EnqueueDeploymentsMatchingNamespaceRegex(regexCode string) {
	s.regexes = append(s.regexes, regexCode)
}

func TestDeploymentHandlers(t *testing.T) {
	t.Run("add enqueues deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentAdd(testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentAdd("not-a-deployment")

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("update enqueues current deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentUpdate(
			testutil.NewDeployment(testutil.TestNamespace, "old"),
			testutil.NewDeployment(testutil.TestNamespace, "new"),
		)

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, "new")})
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("update ignores unexpected new object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onDeploymentUpdate(
			testutil.NewDeployment(testutil.TestNamespace, "old"),
			"not-a-deployment",
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

func TestHPAHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onHPAAdd(testutil.NewHPA("api-hpa", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onHPAAdd("not-an-hpa")

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores non Deployment target", func(t *testing.T) {
		handler, sink := newHandler(t)

		hpa := testutil.NewHPA("api-hpa", testutil.TestNamespace, testutil.TestDeploymentName)
		hpa.Spec.ScaleTargetRef.Kind = "StatefulSet"
		handler.onHPAAdd(hpa)

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("update enqueues old and new targets", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onHPAUpdate(
			testutil.NewHPA("api-hpa", testutil.TestNamespace, "old-api"),
			testutil.NewHPA("api-hpa", testutil.TestNamespace, "new-api"),
		)

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, "old-api"), deploymentKey(testutil.TestNamespace, "new-api")})
	})

	t.Run("update ignores invalid old and enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onHPAUpdate("not-an-hpa", testutil.NewHPA("api-hpa", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
	})

	t.Run("delete is ignored", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onHPADelete(testutil.NewHPA("api-hpa", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})
}

func TestVPAHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAAdd(testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
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

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, "old-api"), deploymentKey(testutil.TestNamespace, "new-api")})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPAUpdate("not-a-vpa", testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVPADelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewVPA(testutil.TestVPAName, testutil.TestNamespace, testutil.TestDeploymentName),
		})

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
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

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{testutil.TestNamespace})
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

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{testutil.TestNamespace})
	})

	t.Run("update ignores unexpected new object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceUpdate(testutil.NewWatchedNamespace("old"), "not-a-watched-namespace")

		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("delete enqueues all deployments in namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(testutil.NewWatchedNamespace(testutil.TestNamespace))

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{testutil.TestNamespace})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewWatchedNamespace(testutil.TestNamespace),
		})

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{testutil.TestNamespace})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{Obj: "not-a-watched-namespace"})

		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})
}

func TestWatchedNamespaceRegexHandlers(t *testing.T) {
	t.Run("add enqueues deployments matching regex", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceRegexAdd(testutil.NewWatchedNamespaceRegex("production", `^prod-.*$`))

		testutil.AssertStringSlicesEqual(t, sink.regexes, []string{`^prod-.*$`})
		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceRegexAdd("not-a-watched-namespace-regex")

		testutil.AssertStringSlicesEqual(t, sink.regexes, nil)
	})

	t.Run("update with changed regex enqueues old and new regexes", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceRegexUpdate(
			testutil.NewWatchedNamespaceRegex("production", `^prod-.*$`),
			testutil.NewWatchedNamespaceRegex("production", `^(prod|staging)-.*$`),
		)

		testutil.AssertStringSlicesEqual(t, sink.regexes, []string{`^prod-.*$`, `^(prod|staging)-.*$`})
	})

	t.Run("update with same regex enqueues once", func(t *testing.T) {
		handler, sink := newHandler(t)

		oldObj := testutil.NewWatchedNamespaceRegex("production", `^prod-.*$`)
		newObj := testutil.NewWatchedNamespaceRegex("production", `^prod-.*$`)
		newObj.Spec.VhapePolicyName = "new-policy"
		handler.onWatchedNamespaceRegexUpdate(oldObj, newObj)

		testutil.AssertStringSlicesEqual(t, sink.regexes, []string{`^prod-.*$`})
	})

	t.Run("update ignores invalid old and enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceRegexUpdate("invalid", testutil.NewWatchedNamespaceRegex("production", `^prod-.*$`))

		testutil.AssertStringSlicesEqual(t, sink.regexes, []string{`^prod-.*$`})
	})

	t.Run("delete enqueues deployments matching regex", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceRegexDelete(testutil.NewWatchedNamespaceRegex("production", `^prod-.*$`))

		testutil.AssertStringSlicesEqual(t, sink.regexes, []string{`^prod-.*$`})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceRegexDelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewWatchedNamespaceRegex("production", `^prod-.*$`),
		})

		testutil.AssertStringSlicesEqual(t, sink.regexes, []string{`^prod-.*$`})
	})

	t.Run("delete ignores invalid tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onWatchedNamespaceRegexDelete(cache.DeletedFinalStateUnknown{Obj: "invalid"})

		testutil.AssertStringSlicesEqual(t, sink.regexes, nil)
	})
}

func TestIgnoredNamespaceHandlers(t *testing.T) {
	t.Run("add enqueues namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredNamespaceAdd(testutil.NewIgnoredNamespace(testutil.TestNamespace))

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{testutil.TestNamespace})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredNamespaceAdd("invalid")

		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("update is ignored", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredNamespaceUpdate(
			testutil.NewIgnoredNamespace(testutil.TestNamespace),
			testutil.NewIgnoredNamespace(testutil.TestNamespace),
		)

		testutil.AssertStringSlicesEqual(t, sink.namespaces, nil)
	})

	t.Run("delete enqueues namespace", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredNamespaceDelete(testutil.NewIgnoredNamespace(testutil.TestNamespace))

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{testutil.TestNamespace})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredNamespaceDelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewIgnoredNamespace(testutil.TestNamespace),
		})

		testutil.AssertStringSlicesEqual(t, sink.namespaces, []string{testutil.TestNamespace})
	})
}

func TestIgnoredWorkloadHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadAdd(testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
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

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, "old-api"), deploymentKey(testutil.TestNamespace, "new-api")})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadUpdate("not-an-ignored-workload", testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName))

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{
			Obj: testutil.NewIgnoredWorkload("ignored", testutil.TestNamespace, testutil.TestDeploymentName),
		})

		testutil.AssertStringSlicesEqual(t, sink.deployments, []string{deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName)})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{Obj: "not-an-ignored-workload"})

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})
}

func TestVhapePolicyHandlers(t *testing.T) {
	t.Run("enqueueDeploymentFromVhapePolicy enqueues eligible Deployment VPAs", func(t *testing.T) {
		handler, sink := newHandler(t)

		matchingVPA := testutil.NewVPA("matching", testutil.TestNamespace, testutil.TestDeploymentName)
		matchingVPA.Annotations = map[string]string{vhapev1alpha1.VhapePolicyAnnotation: testutil.TestPolicyName}
		matchingVPA.Labels = map[string]string{vpaservice.VhapeLabel: "team-a-recommender"}
		addVPAForPolicy(t, handler, matchingVPA)

		otherVPA := testutil.NewVPA("other", testutil.TestNamespace, "other-api")
		otherVPA.Annotations = map[string]string{vhapev1alpha1.VhapePolicyAnnotation: "other-policy"}
		addVPAForPolicy(t, handler, otherVPA)

		vpaWithoutVhapeLabel := testutil.NewVPA("unlabeled", testutil.TestNamespace, "default-api")
		vpaWithoutVhapeLabel.Annotations = map[string]string{vhapev1alpha1.VhapePolicyAnnotation: testutil.TestPolicyName}
		addVPAForPolicy(t, handler, vpaWithoutVhapeLabel)

		vpaWithEmptyVhapeLabel := testutil.NewVPA("empty-label", testutil.TestNamespace, "empty-label-api")
		vpaWithEmptyVhapeLabel.Annotations = map[string]string{vhapev1alpha1.VhapePolicyAnnotation: testutil.TestPolicyName}
		vpaWithEmptyVhapeLabel.Labels = map[string]string{vpaservice.VhapeLabel: ""}
		addVPAForPolicy(t, handler, vpaWithEmptyVhapeLabel)

		nonDeploymentVPA := testutil.NewVPA("stateful", testutil.TestNamespace, "stateful-api")
		nonDeploymentVPA.Annotations = map[string]string{vhapev1alpha1.VhapePolicyAnnotation: testutil.TestPolicyName}
		nonDeploymentVPA.Spec.TargetRef.Kind = "StatefulSet"
		nonDeploymentVPA.Labels = map[string]string{vpaservice.VhapeLabel: "team-b-recommender"}
		addVPAForPolicy(t, handler, nonDeploymentVPA)

		handler.enqueueDeploymentFromVhapePolicy(&vhapev1alpha1.VhapePolicy{ObjectMeta: metav1.ObjectMeta{Name: testutil.TestPolicyName}})

		testutil.AssertStringSlicesEqualIgnoringOrder(t, sink.deployments, []string{
			deploymentKey(testutil.TestNamespace, testutil.TestDeploymentName),
			deploymentKey(testutil.TestNamespace, "empty-label-api"),
		})
	})

	t.Run("enqueueDeploymentFromVhapePolicy without index does nothing", func(t *testing.T) {
		sink := &fakeDeploymentSink{}
		handler, err := New(sink)
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}

		handler.enqueueDeploymentFromVhapePolicy(&vhapev1alpha1.VhapePolicy{ObjectMeta: metav1.ObjectMeta{Name: testutil.TestPolicyName}})

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		handler, sink := newHandler(t)

		handler.onVhapePolicyAdd("not-a-vhape-policy")

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})

	t.Run("update and delete are ignored because policy spec is immutable", func(t *testing.T) {
		handler, sink := newHandler(t)
		policy := &vhapev1alpha1.VhapePolicy{ObjectMeta: metav1.ObjectMeta{Name: testutil.TestPolicyName}}

		handler.onVhapePolicyUpdate(policy, policy)
		handler.onVhapePolicyDelete(policy)

		testutil.AssertStringSlicesEqual(t, sink.deployments, nil)
	})
}

func addVPAForPolicy(t *testing.T, handler *Handler, vpa interface{}) {
	t.Helper()

	if err := handler.vpaIndexer.Add(vpa); err != nil {
		t.Fatalf("add VPA to policy index: %v", err)
	}
}

func deploymentKey(namespace, name string) string {
	return namespace + "/" + name
}

func newHandler(t *testing.T) (*Handler, *fakeDeploymentSink) {
	t.Helper()

	sink := &fakeDeploymentSink{}
	handler, err := New(sink)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	handler.vpaIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		watcherinformers.VPAByVhapePolicyIndex: watcherinformers.GetAssociatedVPAVhapePolicyKey,
	})

	return handler, sink
}
