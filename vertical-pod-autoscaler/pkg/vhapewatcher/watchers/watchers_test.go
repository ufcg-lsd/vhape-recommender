package watchers

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
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

func TestNewWatchers(t *testing.T) {
	t.Run("returns watchers with valid sink", func(t *testing.T) {
		watchers, err := New(&fakeDeploymentSink{})
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if watchers == nil {
			t.Fatal("New() returned nil watchers")
		}
	})

	t.Run("rejects nil sink", func(t *testing.T) {
		watchers, err := New(nil)
		if err == nil {
			t.Fatal("New() expected error, got nil")
		}
		if watchers != nil {
			t.Fatalf("New() returned watchers = %#v, want nil", watchers)
		}
	})
}

func TestDeploymentHandlers(t *testing.T) {
	t.Run("add enqueues deployment", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onDeploymentAdd(newDeployment(testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
		assertNamespaces(t, sink, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onDeploymentAdd("not-a-deployment")

		assertDeployments(t, sink, nil)
		assertNamespaces(t, sink, nil)
	})

	t.Run("update is ignored", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onDeploymentUpdate(
			newDeployment(testNamespace, "old"),
			newDeployment(testNamespace, "new"),
		)

		assertDeployments(t, sink, nil)
		assertNamespaces(t, sink, nil)
	})

	t.Run("delete is ignored", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onDeploymentDelete(newDeployment(testNamespace, testDeploymentName))

		assertDeployments(t, sink, nil)
		assertNamespaces(t, sink, nil)
	})
}

func TestVPAHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onVPAAdd(newVPA("vpa", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onVPAAdd("not-a-vpa")

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores VPA without targetRef", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		vpa := newVPA("vpa", testNamespace, testDeploymentName)
		vpa.Spec.TargetRef = nil
		watchers.onVPAAdd(vpa)

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores non Deployment target", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		vpa := newVPA("vpa", testNamespace, testDeploymentName)
		vpa.Spec.TargetRef.Kind = "StatefulSet"
		watchers.onVPAAdd(vpa)

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores target with empty name", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		vpa := newVPA("vpa", testNamespace, "")
		watchers.onVPAAdd(vpa)

		assertDeployments(t, sink, nil)
	})

	t.Run("update enqueues old and new targets", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onVPAUpdate(
			newVPA("vpa", testNamespace, "old-api"),
			newVPA("vpa", testNamespace, "new-api"),
		)

		assertDeployments(t, sink, []string{"producao/old-api", "producao/new-api"})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onVPAUpdate("not-a-vpa", newVPA("vpa", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onVPADelete(newVPA("vpa", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onVPADelete(cache.DeletedFinalStateUnknown{
			Obj: newVPA("vpa", testNamespace, testDeploymentName),
		})

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onVPADelete(cache.DeletedFinalStateUnknown{Obj: "not-a-vpa"})

		assertDeployments(t, sink, nil)
	})
}

func TestWatchedNamespaceHandlers(t *testing.T) {
	t.Run("add enqueues all deployments in namespace", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onWatchedNamespaceAdd(newWatchedNamespace(testNamespace))

		assertNamespaces(t, sink, []string{"producao"})
		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onWatchedNamespaceAdd("not-a-watched-namespace")

		assertNamespaces(t, sink, nil)
		assertDeployments(t, sink, nil)
	})

	t.Run("update enqueues all deployments in new namespace object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onWatchedNamespaceUpdate(
			newWatchedNamespace("old"),
			newWatchedNamespace(testNamespace),
		)

		assertNamespaces(t, sink, []string{"producao"})
	})

	t.Run("update ignores unexpected new object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onWatchedNamespaceUpdate(newWatchedNamespace("old"), "not-a-watched-namespace")

		assertNamespaces(t, sink, nil)
	})

	t.Run("delete enqueues all deployments in namespace", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onWatchedNamespaceDelete(newWatchedNamespace(testNamespace))

		assertNamespaces(t, sink, []string{"producao"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{
			Obj: newWatchedNamespace(testNamespace),
		})

		assertNamespaces(t, sink, []string{"producao"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onWatchedNamespaceDelete(cache.DeletedFinalStateUnknown{Obj: "not-a-watched-namespace"})

		assertNamespaces(t, sink, nil)
	})
}

func TestIgnoredWorkloadHandlers(t *testing.T) {
	t.Run("add enqueues target deployment", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadAdd(newIgnoredWorkload("ignored", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("add ignores unexpected object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadAdd("not-an-ignored-workload")

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores missing target namespace", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadAdd(newIgnoredWorkload("ignored", "", testDeploymentName))

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores missing target name", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadAdd(newIgnoredWorkload("ignored", testNamespace, ""))

		assertDeployments(t, sink, nil)
	})

	t.Run("add ignores non Deployment target", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		ignored := newIgnoredWorkload("ignored", testNamespace, testDeploymentName)
		ignored.Spec.TargetRef.Kind = "StatefulSet"
		watchers.onIgnoredWorkloadAdd(ignored)

		assertDeployments(t, sink, nil)
	})

	t.Run("update enqueues old and new targets", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadUpdate(
			newIgnoredWorkload("ignored-old", testNamespace, "old-api"),
			newIgnoredWorkload("ignored-new", testNamespace, "new-api"),
		)

		assertDeployments(t, sink, []string{"producao/old-api", "producao/new-api"})
	})

	t.Run("update ignores invalid old and still enqueues new", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadUpdate("not-an-ignored-workload", newIgnoredWorkload("ignored", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete enqueues target deployment", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadDelete(newIgnoredWorkload("ignored", testNamespace, testDeploymentName))

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete handles tombstone", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{
			Obj: newIgnoredWorkload("ignored", testNamespace, testDeploymentName),
		})

		assertDeployments(t, sink, []string{"producao/api"})
	})

	t.Run("delete ignores tombstone with unexpected object", func(t *testing.T) {
		watchers, sink := newWatchers(t)

		watchers.onIgnoredWorkloadDelete(cache.DeletedFinalStateUnknown{Obj: "not-an-ignored-workload"})

		assertDeployments(t, sink, nil)
	})
}

func TestIsDeploymentTarget(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		kind       string
		targetName string
		want       bool
	}{
		{
			name:       "valid deployment target",
			apiVersion: deploymentAPIVersion,
			kind:       deploymentKind,
			targetName: testDeploymentName,
			want:       true,
		},
		{
			name:       "kind is case insensitive",
			apiVersion: deploymentAPIVersion,
			kind:       "deployment",
			targetName: testDeploymentName,
			want:       true,
		},
		{
			name:       "wrong apiVersion",
			apiVersion: "extensions/v1beta1",
			kind:       deploymentKind,
			targetName: testDeploymentName,
			want:       false,
		},
		{
			name:       "wrong kind",
			apiVersion: deploymentAPIVersion,
			kind:       "StatefulSet",
			targetName: testDeploymentName,
			want:       false,
		},
		{
			name:       "empty name",
			apiVersion: deploymentAPIVersion,
			kind:       deploymentKind,
			targetName: "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDeploymentTarget(tt.apiVersion, tt.kind, tt.targetName)
			if got != tt.want {
				t.Fatalf("isDeploymentTarget(%q, %q, %q) = %v, want %v", tt.apiVersion, tt.kind, tt.targetName, got, tt.want)
			}
		})
	}
}

func newWatchers(t *testing.T) (*Watchers, *fakeDeploymentSink) {
	t.Helper()

	sink := &fakeDeploymentSink{}
	watchers, err := New(sink)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return watchers, sink
}

func newDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func newVPA(name, namespace, targetName string) *vpav1.VerticalPodAutoscaler {
	return &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				APIVersion: deploymentAPIVersion,
				Kind:       deploymentKind,
				Name:       targetName,
			},
		},
	}
}

func newWatchedNamespace(name string) *vhapev1alpha1.VhapeWatchedNamespace {
	return &vhapev1alpha1.VhapeWatchedNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newIgnoredWorkload(name, targetNamespace, targetName string) *vhapev1alpha1.VhapeIgnoredWorkload {
	return &vhapev1alpha1.VhapeIgnoredWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: vhapev1alpha1.VhapeIgnoredWorkloadSpec{
			TargetRef: vhapev1alpha1.TargetRef{
				APIVersion: deploymentAPIVersion,
				Kind:       deploymentKind,
				Namespace:  targetNamespace,
				Name:       targetName,
			},
			Reason: "test",
		},
	}
}

func assertDeployments(t *testing.T, sink *fakeDeploymentSink, want []string) {
	t.Helper()

	if want == nil {
		want = []string{}
	}
	if sink.deployments == nil {
		sink.deployments = []string{}
	}
	if !reflect.DeepEqual(sink.deployments, want) {
		t.Fatalf("enqueued deployments = %#v, want %#v", sink.deployments, want)
	}
}

func assertNamespaces(t *testing.T, sink *fakeDeploymentSink, want []string) {
	t.Helper()

	if want == nil {
		want = []string{}
	}
	if sink.namespaces == nil {
		sink.namespaces = []string{}
	}
	if !reflect.DeepEqual(sink.namespaces, want) {
		t.Fatalf("enqueued namespaces = %#v, want %#v", sink.namespaces, want)
	}
}
