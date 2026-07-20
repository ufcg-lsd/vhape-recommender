package scope

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/cache"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	vhapelisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/autoscaling.vhape.io/v1alpha1"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
)

func TestNewScopeResolverFromListers(t *testing.T) {
	watchedNamespaceLister, ignoredWorkloadLister := testutil.NewVhapeListers(t, nil, nil)

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
	addWatchedNamespace(t, watchedNamespaceIndexer, testutil.TestNamespace)

	t.Run("returns watched namespace from cache", func(t *testing.T) {
		watched, err := scope.GetWatchedNamespace(testutil.TestNamespace)
		if err != nil {
			t.Fatalf("GetWatchedNamespace() returned error: %v", err)
		}
		if watched == nil {
			t.Fatal("GetWatchedNamespace() returned nil")
		}
		if watched.Name != testutil.TestNamespace {
			t.Fatalf("GetWatchedNamespace() name = %q, want %q", watched.Name, testutil.TestNamespace)
		}

		testutil.AssertWatchedNamespaceSpec(
			t,
			watched,
			testutil.TestPolicyNamespace,
			testutil.TestPolicyName,
			testutil.TestVPAUpdateMode,
		)
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
		ignoredWorkloads  []*vhapev1alpha1.VhapeIgnoredWorkload
		deployment        *appsv1.Deployment
		wantShouldManage  bool
		wantReason        string
		wantWatched       string
	}{
		{
			name:              "watched namespace manages deployment",
			watchedNamespaces: []string{testutil.TestNamespace},
			deployment:        testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage:  true,
			wantReason:        ReasonWatched,
			wantWatched:       testutil.TestNamespace,
		},
		{
			name:             "namespace not watched skips deployment",
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonNamespaceNotWatched,
		},
		{
			name:              "ignored workload wins over watched namespace",
			watchedNamespaces: []string{testutil.TestNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkload("ignore-api", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonWorkloadIgnored,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name:              "ignored workload kind is case insensitive",
			watchedNamespaces: []string{testutil.TestNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", "apps/v1", "deployment", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonWorkloadIgnored,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name:              "ignored workload with different apiVersion does not match",
			watchedNamespaces: []string{testutil.TestNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", "batch/v1", "Deployment", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name:              "ignored workload with different kind does not match",
			watchedNamespaces: []string{testutil.TestNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", "apps/v1", "StatefulSet", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name:              "ignored workload with different namespace does not match",
			watchedNamespaces: []string{testutil.TestNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", "apps/v1", "Deployment", "staging", testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name:              "ignored workload with different name does not match",
			watchedNamespaces: []string{testutil.TestNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-worker", "apps/v1", "Deployment", testutil.TestNamespace, "worker"),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, watchedNamespaceIndexer, ignoredWorkloadIndexer := newScope(t)

			for _, namespace := range tt.watchedNamespaces {
				addWatchedNamespace(t, watchedNamespaceIndexer, namespace)
			}
			for _, ignored := range tt.ignoredWorkloads {
				testutil.AddToIndexer(t, ignoredWorkloadIndexer, ignored)
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

			testutil.AssertWatchedNamespaceSpec(
				t,
				decision.WatchedNamespace,
				testutil.TestPolicyNamespace,
				testutil.TestPolicyName,
				testutil.TestVPAUpdateMode,
			)
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
		ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload
		deployment       *appsv1.Deployment
		want             bool
	}{
		{
			name: "returns true when ignored workload targets deployment",
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkload("ignore-api", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment: testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			want:       true,
		},
		{
			name:       "returns false when ignored workload list is empty",
			deployment: testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			want:       false,
		},
		{
			name: "returns false when targetRef is incomplete",
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", "apps/v1", "Deployment", "", testutil.TestDeploymentName),
			},
			deployment: testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, _, ignoredWorkloadIndexer := newScope(t)

			for _, ignored := range tt.ignoredWorkloads {
				testutil.AddToIndexer(t, ignoredWorkloadIndexer, ignored)
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

	watchedNamespaceIndexer := testutil.NewIndexer()
	ignoredWorkloadIndexer := testutil.NewIndexer()

	watchedNamespaceLister := vhapelisters.NewVhapeWatchedNamespaceLister(watchedNamespaceIndexer)
	ignoredWorkloadLister := vhapelisters.NewVhapeIgnoredWorkloadLister(ignoredWorkloadIndexer)

	scope, err := New(watchedNamespaceLister, ignoredWorkloadLister)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return scope, watchedNamespaceIndexer, ignoredWorkloadIndexer
}

func addWatchedNamespace(t *testing.T, indexer cache.Indexer, namespace string) *vhapev1alpha1.VhapeWatchedNamespace {
	t.Helper()

	watched := testutil.NewWatchedNamespace(namespace)
	testutil.AddToIndexer(t, indexer, watched)

	return watched
}
