package scope

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vhapelisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.vhape.io/v1alpha1"
)

const (
	testPolicyNamespace = "kube-system"
	testPolicyName      = "vhape-policy-p93-default"
	testVPAUpdateMode   = "InPlaceOrRecreate"
)

func TestNewScopeResolverFromListers(t *testing.T) {
	watchedNamespaceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	ignoredWorkloadIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	watchedNamespaceLister := vhapelisters.NewVhapeWatchedNamespaceLister(watchedNamespaceIndexer)
	ignoredWorkloadLister := vhapelisters.NewVhapeIgnoredWorkloadLister(ignoredWorkloadIndexer)

	t.Run("returns scope with valid listers", func(t *testing.T) {
		scope, err := New(watchedNamespaceLister, ignoredWorkloadLister)
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if scope == nil {
			t.Fatal("New() returned nil scope")
		}
	})

	t.Run("rejects nil watched namespace lister", func(t *testing.T) {
		_, err := New(nil, ignoredWorkloadLister)
		if err == nil {
			t.Fatal("New() expected error, got nil")
		}
	})

	t.Run("rejects nil ignored workload lister", func(t *testing.T) {
		_, err := New(watchedNamespaceLister, nil)
		if err == nil {
			t.Fatal("New() expected error, got nil")
		}
	})
}

func TestGetWatchedNamespace(t *testing.T) {
	scope, watchedNamespaceIndexer, _ := newScope(t)
	addWatchedNamespace(t, watchedNamespaceIndexer, "producao")

	t.Run("returns watched namespace from cache", func(t *testing.T) {
		watched, err := scope.GetWatchedNamespace("producao")
		if err != nil {
			t.Fatalf("GetWatchedNamespace() returned error: %v", err)
		}
		if watched == nil {
			t.Fatal("GetWatchedNamespace() returned nil")
		}
		if watched.Name != "producao" {
			t.Fatalf("GetWatchedNamespace() name = %q, want %q", watched.Name, "producao")
		}

		assertWatchedNamespaceSpec(t, watched)
	})

	t.Run("returns nil for namespace not watched", func(t *testing.T) {
		watched, err := scope.GetWatchedNamespace("notindexed")
		if err != nil {
			t.Fatalf("GetWatchedNamespace() returned error: %v", err)
		}
		if watched != nil {
			t.Fatalf("GetWatchedNamespace() returned %#v, want nil", watched)
		}
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		_, err := scope.GetWatchedNamespace("")
		if err == nil {
			t.Fatal("GetWatchedNamespace() expected error, got nil")
		}
	})
}

func TestShouldManageDeployment(t *testing.T) {
	tests := []struct {
		name              string
		watchedNamespaces []string
		ignoredWorkloads  []vhapev1alpha1.VhapeIgnoredWorkload
		deployment        *appsv1.Deployment
		wantShouldManage  bool
		wantReason        string
		wantWatched       string
	}{
		{
			name:              "watched namespace manages deployment",
			watchedNamespaces: []string{"producao"},
			deployment:        deployment("producao", "api"),
			wantShouldManage:  true,
			wantReason:        ReasonWatched,
			wantWatched:       "producao",
		},
		{
			name:             "namespace not watched skips deployment",
			deployment:       deployment("producao", "api"),
			wantShouldManage: false,
			wantReason:       ReasonNamespaceNotWatched,
		},
		{
			name:              "ignored workload wins over watched namespace",
			watchedNamespaces: []string{"producao"},
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-api", targetRef("apps/v1", "Deployment", "producao", "api")),
			},
			deployment:       deployment("producao", "api"),
			wantShouldManage: false,
			wantReason:       ReasonWorkloadIgnored,
			wantWatched:      "producao",
		},
		{
			name:              "ignored workload kind is case insensitive",
			watchedNamespaces: []string{"producao"},
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-api", targetRef("apps/v1", "deployment", "producao", "api")),
			},
			deployment:       deployment("producao", "api"),
			wantShouldManage: false,
			wantReason:       ReasonWorkloadIgnored,
			wantWatched:      "producao",
		},
		{
			name:              "ignored workload with different apiVersion does not match",
			watchedNamespaces: []string{"producao"},
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-api", targetRef("batch/v1", "Deployment", "producao", "api")),
			},
			deployment:       deployment("producao", "api"),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      "producao",
		},
		{
			name:              "ignored workload with different kind does not match",
			watchedNamespaces: []string{"producao"},
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-api", targetRef("apps/v1", "StatefulSet", "producao", "api")),
			},
			deployment:       deployment("producao", "api"),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      "producao",
		},
		{
			name:              "ignored workload with different namespace does not match",
			watchedNamespaces: []string{"producao"},
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-api", targetRef("apps/v1", "Deployment", "staging", "api")),
			},
			deployment:       deployment("producao", "api"),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      "producao",
		},
		{
			name:              "ignored workload with different name does not match",
			watchedNamespaces: []string{"producao"},
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-worker", targetRef("apps/v1", "Deployment", "producao", "worker")),
			},
			deployment:       deployment("producao", "api"),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      "producao",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, watchedNamespaceIndexer, ignoredWorkloadIndexer := newScope(t)

			for _, namespace := range tt.watchedNamespaces {
				addWatchedNamespace(t, watchedNamespaceIndexer, namespace)
			}
			for i := range tt.ignoredWorkloads {
				addIgnoredWorkload(t, ignoredWorkloadIndexer, &tt.ignoredWorkloads[i])
			}

			decision, err := scope.ShouldManageDeployment(tt.deployment)
			if err != nil {
				t.Fatalf("ShouldManageDeployment() returned error: %v", err)
			}

			if decision.ShouldManage != tt.wantShouldManage {
				t.Fatalf("ShouldManageDeployment().ShouldManage = %v, want %v", decision.ShouldManage, tt.wantShouldManage)
			}
			if decision.Reason != tt.wantReason {
				t.Fatalf("ShouldManageDeployment().Reason = %q, want %q", decision.Reason, tt.wantReason)
			}

			if tt.wantWatched == "" {
				if decision.WatchedNamespace != nil {
					t.Fatalf("ShouldManageDeployment().WatchedNamespace = %#v, want nil", decision.WatchedNamespace)
				}
				return
			}

			if decision.WatchedNamespace == nil {
				t.Fatalf("ShouldManageDeployment().WatchedNamespace = nil, want %q", tt.wantWatched)
			}
			if decision.WatchedNamespace.Name != tt.wantWatched {
				t.Fatalf("ShouldManageDeployment().WatchedNamespace.Name = %q, want %q", decision.WatchedNamespace.Name, tt.wantWatched)
			}

			assertWatchedNamespaceSpec(t, decision.WatchedNamespace)
		})
	}
}

func TestShouldManageDeploymentRejectsNilDeployment(t *testing.T) {
	scope, _, _ := newScope(t)

	_, err := scope.ShouldManageDeployment(nil)
	if err == nil {
		t.Fatal("ShouldManageDeployment() expected error, got nil")
	}
}

func TestIsDeploymentIgnored(t *testing.T) {
	tests := []struct {
		name             string
		ignoredWorkloads []vhapev1alpha1.VhapeIgnoredWorkload
		deployment       *appsv1.Deployment
		want             bool
	}{
		{
			name: "returns true when ignored workload targets deployment",
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-api", targetRef("apps/v1", "Deployment", "producao", "api")),
			},
			deployment: deployment("producao", "api"),
			want:       true,
		},
		{
			name:       "returns false when ignored workload list is empty",
			deployment: deployment("producao", "api"),
			want:       false,
		},
		{
			name: "returns false when targetRef is incomplete",
			ignoredWorkloads: []vhapev1alpha1.VhapeIgnoredWorkload{
				ignoredWorkload("ignore-api", targetRef("apps/v1", "Deployment", "", "api")),
			},
			deployment: deployment("producao", "api"),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, _, ignoredWorkloadIndexer := newScope(t)

			for i := range tt.ignoredWorkloads {
				addIgnoredWorkload(t, ignoredWorkloadIndexer, &tt.ignoredWorkloads[i])
			}

			got, err := scope.IsDeploymentIgnored(tt.deployment)
			if err != nil {
				t.Fatalf("IsDeploymentIgnored() returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("IsDeploymentIgnored() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDeploymentIgnoredRejectsNilDeployment(t *testing.T) {
	scope, _, _ := newScope(t)

	_, err := scope.IsDeploymentIgnored(nil)
	if err == nil {
		t.Fatal("IsDeploymentIgnored() expected error, got nil")
	}
}

func newScope(t *testing.T) (*Scope, cache.Indexer, cache.Indexer) {
	t.Helper()

	watchedNamespaceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	ignoredWorkloadIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	watchedNamespaceLister := vhapelisters.NewVhapeWatchedNamespaceLister(watchedNamespaceIndexer)
	ignoredWorkloadLister := vhapelisters.NewVhapeIgnoredWorkloadLister(ignoredWorkloadIndexer)

	scope, err := New(watchedNamespaceLister, ignoredWorkloadLister)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return scope, watchedNamespaceIndexer, ignoredWorkloadIndexer
}

func deployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func addWatchedNamespace(t *testing.T, indexer cache.Indexer, namespace string) *vhapev1alpha1.VhapeWatchedNamespace {
	t.Helper()

	watched := &vhapev1alpha1.VhapeWatchedNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	watched.Spec.VhapePolicyRef.Namespace = testPolicyNamespace
	watched.Spec.VhapePolicyRef.Name = testPolicyName
	watched.Spec.VPAUpdateMode = testVPAUpdateMode

	if err := indexer.Add(watched); err != nil {
		t.Fatalf("add watched namespace to indexer: %v", err)
	}

	return watched
}

func assertWatchedNamespaceSpec(t *testing.T, watched *vhapev1alpha1.VhapeWatchedNamespace) {
	t.Helper()

	if watched == nil {
		t.Fatal("watched namespace is nil")
	}

	if watched.Spec.VhapePolicyRef.Namespace != testPolicyNamespace {
		t.Fatalf(
			"VhapePolicyRef.Namespace = %q, want %q",
			watched.Spec.VhapePolicyRef.Namespace,
			testPolicyNamespace,
		)
	}
	if watched.Spec.VhapePolicyRef.Name != testPolicyName {
		t.Fatalf(
			"VhapePolicyRef.Name = %q, want %q",
			watched.Spec.VhapePolicyRef.Name,
			testPolicyName,
		)
	}
	if watched.Spec.VPAUpdateMode != testVPAUpdateMode {
		t.Fatalf(
			"VPAUpdateMode = %q, want %q",
			watched.Spec.VPAUpdateMode,
			testVPAUpdateMode,
		)
	}
}

func ignoredWorkload(name string, ref vhapev1alpha1.TargetRef) vhapev1alpha1.VhapeIgnoredWorkload {
	return vhapev1alpha1.VhapeIgnoredWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: vhapev1alpha1.VhapeIgnoredWorkloadSpec{
			TargetRef: ref,
		},
	}
}

func addIgnoredWorkload(t *testing.T, indexer cache.Indexer, ignored *vhapev1alpha1.VhapeIgnoredWorkload) {
	t.Helper()

	if err := indexer.Add(ignored); err != nil {
		t.Fatalf("add ignored workload to indexer: %v", err)
	}
}

func targetRef(apiVersion, kind, namespace, name string) vhapev1alpha1.TargetRef {
	return vhapev1alpha1.TargetRef{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}
}
