package scope

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
)

func TestNewScopeResolverFromListers(t *testing.T) {
	_, _, informers := testutil.NewInformers(t, nil, nil, nil, nil)
	watchedNamespaceLister := informers.VhapeWatchedNamespace.Lister()
	ignoredWorkloadLister := informers.VhapeIgnoredWorkload.Lister()

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
	watchedNamespaces := []*vhapev1alpha1.VhapeWatchedNamespace{testutil.NewWatchedNamespace(testutil.TestNamespace)}
	scope := newScope(t, watchedNamespaces, nil)

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
		watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace
		ignoredWorkloads  []*vhapev1alpha1.VhapeIgnoredWorkload
		deployment        *appsv1.Deployment
		wantShouldManage  bool
		wantReason        string
		wantWatched       string
	}{
		{
			name: "watched namespace manages deployment",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{
				testutil.NewWatchedNamespace(testutil.TestNamespace),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name:             "namespace not watched skips deployment",
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonNamespaceNotWatched,
		},
		{
			name: "ignored workload wins over watched namespace",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{
				testutil.NewWatchedNamespace(testutil.TestNamespace),
			},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkload("ignore-api", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonWorkloadIgnored,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name: "ignored workload with different apiVersion does not match",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{
				testutil.NewWatchedNamespace(testutil.TestNamespace),
			},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", "batch/v1", "Deployment", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name: "ignored workload with different kind does not match",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{
				testutil.NewWatchedNamespace(testutil.TestNamespace),
			},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", appsv1.SchemeGroupVersion.String(), "StatefulSet", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name: "ignored workload with different namespace does not match",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{
				testutil.NewWatchedNamespace(testutil.TestNamespace),
			},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", appsv1.SchemeGroupVersion.String(), "Deployment", "staging", testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
		{
			name: "ignored workload with different name does not match",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{
				testutil.NewWatchedNamespace(testutil.TestNamespace),
			},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-worker", appsv1.SchemeGroupVersion.String(), "Deployment", testutil.TestNamespace, "worker"),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantWatched:      testutil.TestNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := newScope(t, tt.watchedNamespaces, tt.ignoredWorkloads)

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
	scope := newScope(t, nil, nil)

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
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", appsv1.SchemeGroupVersion.String(), "Deployment", "", testutil.TestDeploymentName),
			},
			deployment: testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := newScope(t, nil, tt.ignoredWorkloads)

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
	scope := newScope(t, nil, nil)

	_, err := scope.IsDeploymentIgnored(nil)
	if err == nil {
		t.Fatal("IsDeploymentIgnored() expected error, got nil")
	}
}

func newScope(
	t *testing.T,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
) *Scope {
	t.Helper()

	_, _, informers := testutil.NewInformers(t, nil, watchedNamespaces, ignoredWorkloads, nil)

	scope, err := New(
		informers.VhapeWatchedNamespace.Lister(),
		informers.VhapeIgnoredWorkload.Lister(),
	)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return scope
}
