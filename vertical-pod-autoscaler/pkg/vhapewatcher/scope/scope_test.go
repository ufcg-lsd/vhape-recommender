package scope

import (
	"reflect"
	"sort"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
)

func TestNewScopeResolverFromListers(t *testing.T) {
	_, _, informers := testutil.NewInformers(t, nil, nil, nil, nil, nil, nil, nil)
	watchedNamespaceLister := informers.VhapeWatchedNamespace.Lister()
	ignoredWorkloadLister := informers.VhapeIgnoredWorkload.Lister()
	watchedNamespaceRegexLister := informers.VhapeWatchedNamespaceRegex.Lister()
	ignoredNamespaceLister := informers.VhapeIgnoredNamespace.Lister()
	namespaceLister := informers.Namespace.Lister()

	t.Run("returns scope with valid listers", func(t *testing.T) {
		scope, err := New(
			watchedNamespaceLister,
			ignoredWorkloadLister,
			watchedNamespaceRegexLister,
			ignoredNamespaceLister,
			namespaceLister,
		)
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if scope == nil {
			t.Fatal("New() returned nil scope")
		}
	})

	tests := []struct {
		name                        string
		watchedNamespaceLister      bool
		ignoredWorkloadLister       bool
		watchedNamespaceRegexLister bool
		ignoredNamespaceLister      bool
		namespaceLister             bool
	}{
		{name: "rejects nil watched namespace lister", ignoredWorkloadLister: true, watchedNamespaceRegexLister: true, ignoredNamespaceLister: true, namespaceLister: true},
		{name: "rejects nil ignored workload lister", watchedNamespaceLister: true, watchedNamespaceRegexLister: true, ignoredNamespaceLister: true, namespaceLister: true},
		{name: "rejects nil watched namespace regex lister", watchedNamespaceLister: true, ignoredWorkloadLister: true, ignoredNamespaceLister: true, namespaceLister: true},
		{name: "rejects nil ignored namespace lister", watchedNamespaceLister: true, ignoredWorkloadLister: true, watchedNamespaceRegexLister: true, namespaceLister: true},
		{name: "rejects nil namespace lister", watchedNamespaceLister: true, ignoredWorkloadLister: true, watchedNamespaceRegexLister: true, ignoredNamespaceLister: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var watched = watchedNamespaceLister
			var ignoredWorkload = ignoredWorkloadLister
			var watchedRegex = watchedNamespaceRegexLister
			var ignoredNamespace = ignoredNamespaceLister
			var namespace = namespaceLister

			if !tt.watchedNamespaceLister {
				watched = nil
			}
			if !tt.ignoredWorkloadLister {
				ignoredWorkload = nil
			}
			if !tt.watchedNamespaceRegexLister {
				watchedRegex = nil
			}
			if !tt.ignoredNamespaceLister {
				ignoredNamespace = nil
			}
			if !tt.namespaceLister {
				namespace = nil
			}

			if _, err := New(watched, ignoredWorkload, watchedRegex, ignoredNamespace, namespace); err == nil {
				t.Fatal("New() expected error, got nil")
			}
		})
	}
}

func TestGetNamespacesMatchingRegex(t *testing.T) {
	namespaces := []*corev1.Namespace{
		testutil.NewNamespace("production-api"),
		testutil.NewNamespace("production-worker"),
		testutil.NewNamespace("staging-api"),
		testutil.NewNamespace("kube-system"),
	}
	scope := newScope(t, namespaces, nil, nil, nil, nil)

	t.Run("returns only matching namespaces", func(t *testing.T) {
		got, err := scope.GetNamespacesMatchingRegex(`^production-.*$`)
		if err != nil {
			t.Fatalf("GetNamespacesMatchingRegex() returned error: %v", err)
		}

		gotNames := namespaceNames(got)
		wantNames := []string{"production-api", "production-worker"}
		if !reflect.DeepEqual(gotNames, wantNames) {
			t.Fatalf("GetNamespacesMatchingRegex() names = %#v, want %#v", gotNames, wantNames)
		}
	})

	t.Run("returns multiple namespace groups", func(t *testing.T) {
		got, err := scope.GetNamespacesMatchingRegex(`^(production|staging)-.*$`)
		if err != nil {
			t.Fatalf("GetNamespacesMatchingRegex() returned error: %v", err)
		}

		gotNames := namespaceNames(got)
		wantNames := []string{"production-api", "production-worker", "staging-api"}
		if !reflect.DeepEqual(gotNames, wantNames) {
			t.Fatalf("GetNamespacesMatchingRegex() names = %#v, want %#v", gotNames, wantNames)
		}
	})

	t.Run("returns empty slice when nothing matches", func(t *testing.T) {
		got, err := scope.GetNamespacesMatchingRegex(`^development-.*$`)
		if err != nil {
			t.Fatalf("GetNamespacesMatchingRegex() returned error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("GetNamespacesMatchingRegex() len = %d, want 0", len(got))
		}
	})

	t.Run("rejects invalid regex", func(t *testing.T) {
		if _, err := scope.GetNamespacesMatchingRegex(`[invalid`); err == nil {
			t.Fatal("GetNamespacesMatchingRegex() expected error, got nil")
		}
	})

	t.Run("rejects empty regex", func(t *testing.T) {
		if _, err := scope.GetNamespacesMatchingRegex(""); err == nil {
			t.Fatal("GetNamespacesMatchingRegex() expected error, got nil")
		}
	})
}

func TestGetWatchedNamespace(t *testing.T) {
	watchedNamespaces := []*vhapev1alpha1.VhapeWatchedNamespace{testutil.NewWatchedNamespace(testutil.TestNamespace)}
	scope := newScope(t, nil, watchedNamespaces, nil, nil, nil)

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
	}{
		{
			name: "watched namespace manages deployment",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{
				testutil.NewWatchedNamespace(testutil.TestNamespace),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := newScope(t, nil, tt.watchedNamespaces, nil, nil, tt.ignoredWorkloads)

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

			if !tt.wantShouldManage {
				if !reflect.DeepEqual(decision.DesiredConfig, vhapev1alpha1.VhapeWatchedNamespaceSpec{}) {
					t.Fatalf("ShouldManageDeployment().DesiredConfig = %#v, want zero value", decision.DesiredConfig)
				}
				return
			}

			wantConfig := testutil.NewWatchedNamespace(tt.deployment.Namespace).Spec
			if !reflect.DeepEqual(decision.DesiredConfig, wantConfig) {
				t.Fatalf("ShouldManageDeployment().DesiredConfig = %#v, want %#v", decision.DesiredConfig, wantConfig)
			}
		})
	}
}

func TestShouldManageDeploymentRejectsNilDeployment(t *testing.T) {
	scope := newScope(t, nil, nil, nil, nil, nil)

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
			scope := newScope(t, nil, nil, nil, nil, tt.ignoredWorkloads)

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
	scope := newScope(t, nil, nil, nil, nil, nil)

	_, err := scope.IsDeploymentIgnored(nil)
	if err == nil {
		t.Fatal("IsDeploymentIgnored() expected error, got nil")
	}
}

func newScope(
	t *testing.T,
	namespaces []*corev1.Namespace,
	watchedNamespaces []*vhapev1alpha1.VhapeWatchedNamespace,
	watchedNamespaceRegexes []*vhapev1alpha1.VhapeWatchedNamespaceRegex,
	ignoredNamespaces []*vhapev1alpha1.VhapeIgnoredNamespace,
	ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload,
) *Scope {
	t.Helper()

	_, _, informers := testutil.NewInformers(
		t,
		nil,
		namespaces,
		watchedNamespaces,
		watchedNamespaceRegexes,
		ignoredNamespaces,
		ignoredWorkloads,
		nil,
	)

	scope, err := New(
		informers.VhapeWatchedNamespace.Lister(),
		informers.VhapeIgnoredWorkload.Lister(),
		informers.VhapeWatchedNamespaceRegex.Lister(),
		informers.VhapeIgnoredNamespace.Lister(),
		informers.Namespace.Lister(),
	)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return scope
}

func namespaceNames(namespaces []*corev1.Namespace) []string {
	names := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		if namespace != nil {
			names = append(names, namespace.Name)
		}
	}
	sort.Strings(names)
	return names
}
