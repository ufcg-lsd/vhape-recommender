package scope

import (
	"reflect"
	"sort"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vhapev1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	watcherinformers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/informers"
	testutil "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/testutil"
)

func TestNewScopeResolverFromCaches(t *testing.T) {
	_, informerSet := testutil.NewInformers(t, nil, nil, nil, nil, nil, nil, nil)

	t.Run("returns scope with valid caches", func(t *testing.T) {
		scope, err := New(informerSet)
		if err != nil {
			t.Fatalf("New() returned error: %v", err)
		}
		if scope == nil {
			t.Fatal("New() returned nil scope")
		}
	})

	t.Run("rejects nil informers", func(t *testing.T) {
		if _, err := New(nil); err == nil {
			t.Fatal("New() expected error, got nil")
		}
	})

	tests := []struct {
		name  string
		clear func(*watcherinformers.Informers)
	}{
		{
			name: "rejects nil Namespace informer",
			clear: func(informers *watcherinformers.Informers) {
				informers.Namespace = nil
			},
		},
		{
			name: "rejects nil VhapeWatchedNamespace informer",
			clear: func(informers *watcherinformers.Informers) {
				informers.VhapeWatchedNamespace = nil
			},
		},
		{
			name: "rejects nil VhapeWatchedNamespaceRegex informer",
			clear: func(informers *watcherinformers.Informers) {
				informers.VhapeWatchedNamespaceRegex = nil
			},
		},
		{
			name: "rejects nil VhapeIgnoredNamespace informer",
			clear: func(informers *watcherinformers.Informers) {
				informers.VhapeIgnoredNamespace = nil
			},
		},
		{
			name: "rejects nil VhapeIgnoredWorkload informer",
			clear: func(informers *watcherinformers.Informers) {
				informers.VhapeIgnoredWorkload = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broken := *informerSet
			tt.clear(&broken)

			if _, err := New(&broken); err == nil {
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

func TestGetWatchedNamespaceRegexesMatchingNamespace(t *testing.T) {
	productionRegex := testutil.NewWatchedNamespaceRegex("production", `^production-.*$`)
	allAppsRegex := testutil.NewWatchedNamespaceRegex("all-apps", `^(production|staging)-.*$`)
	stagingRegex := testutil.NewWatchedNamespaceRegex("staging", `^staging-.*$`)

	scope := newScope(t, nil, nil, []*vhapev1alpha1.VhapeWatchedNamespaceRegex{
		productionRegex,
		allAppsRegex,
		stagingRegex,
	}, nil, nil)

	t.Run("returns only matching regex resources", func(t *testing.T) {
		got, err := scope.GetWatchedNamespaceRegexesMatchingNamespace("production-api")
		if err != nil {
			t.Fatalf("GetWatchedNamespaceRegexesMatchingNamespace() returned error: %v", err)
		}

		gotNames := watchedNamespaceRegexNames(got)
		wantNames := []string{"all-apps", "production"}
		if !reflect.DeepEqual(gotNames, wantNames) {
			t.Fatalf("GetWatchedNamespaceRegexesMatchingNamespace() names = %#v, want %#v", gotNames, wantNames)
		}
	})

	t.Run("returns empty slice when nothing matches", func(t *testing.T) {
		got, err := scope.GetWatchedNamespaceRegexesMatchingNamespace("development-api")
		if err != nil {
			t.Fatalf("GetWatchedNamespaceRegexesMatchingNamespace() returned error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("GetWatchedNamespaceRegexesMatchingNamespace() len = %d, want 0", len(got))
		}
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		if _, err := scope.GetWatchedNamespaceRegexesMatchingNamespace(""); err == nil {
			t.Fatal("GetWatchedNamespaceRegexesMatchingNamespace() expected error, got nil")
		}
	})

	t.Run("returns error for invalid regex in cache", func(t *testing.T) {
		invalidScope := newScope(t, nil, nil, []*vhapev1alpha1.VhapeWatchedNamespaceRegex{
			testutil.NewWatchedNamespaceRegex("invalid", `[invalid`),
		}, nil, nil)

		if _, err := invalidScope.GetWatchedNamespaceRegexesMatchingNamespace("production-api"); err == nil {
			t.Fatal("GetWatchedNamespaceRegexesMatchingNamespace() expected error, got nil")
		}
	})
}

func TestGetIgnoredNamespace(t *testing.T) {
	scope := newScope(t, nil, nil, nil, []*vhapev1alpha1.VhapeIgnoredNamespace{
		testutil.NewIgnoredNamespace(testutil.TestNamespace),
	}, nil)

	t.Run("returns ignored namespace from cache", func(t *testing.T) {
		ignored, err := scope.GetIgnoredNamespace(testutil.TestNamespace)
		if err != nil {
			t.Fatalf("GetIgnoredNamespace() returned error: %v", err)
		}
		if ignored == nil {
			t.Fatal("GetIgnoredNamespace() returned nil")
		}
		if ignored.Name != testutil.TestNamespace {
			t.Fatalf("GetIgnoredNamespace() name = %q, want %q", ignored.Name, testutil.TestNamespace)
		}
	})

	t.Run("returns nil for namespace not ignored", func(t *testing.T) {
		ignored, err := scope.GetIgnoredNamespace("not-ignored")
		if err != nil {
			t.Fatalf("GetIgnoredNamespace() returned error: %v", err)
		}
		if ignored != nil {
			t.Fatalf("GetIgnoredNamespace() returned %#v, want nil", ignored)
		}
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		if _, err := scope.GetIgnoredNamespace(""); err == nil {
			t.Fatal("GetIgnoredNamespace() expected error, got nil")
		}
	})
}

func TestOldestWatchedNamespaceRegex(t *testing.T) {
	t.Run("returns nil for empty slice", func(t *testing.T) {
		if got := oldestWatchedNamespaceRegex(nil); got != nil {
			t.Fatalf("oldestWatchedNamespaceRegex() = %#v, want nil", got)
		}
	})

	t.Run("returns oldest resource", func(t *testing.T) {
		oldest := testutil.NewWatchedNamespaceRegex("oldest", `^production-.*$`)
		oldest.CreationTimestamp = metav1.NewTime(time.Unix(100, 0))

		middle := testutil.NewWatchedNamespaceRegex("middle", `^production-.*$`)
		middle.CreationTimestamp = metav1.NewTime(time.Unix(200, 0))

		newest := testutil.NewWatchedNamespaceRegex("newest", `^production-.*$`)
		newest.CreationTimestamp = metav1.NewTime(time.Unix(300, 0))

		got := oldestWatchedNamespaceRegex([]*vhapev1alpha1.VhapeWatchedNamespaceRegex{middle, newest, oldest})
		if got != oldest {
			t.Fatalf("oldestWatchedNamespaceRegex() = %q, want %q", got.Name, oldest.Name)
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
	watchedNamespace := testutil.NewWatchedNamespace(testutil.TestNamespace)

	regex := testutil.NewWatchedNamespaceRegex("production-regex", `^producao$`)
	regex.Spec.VhapePolicyRef.Name = "regex-policy"

	oldestRegex := testutil.NewWatchedNamespaceRegex("oldest-regex", `^producao$`)
	oldestRegex.CreationTimestamp = metav1.NewTime(time.Unix(100, 0))
	oldestRegex.Spec.VhapePolicyRef.Name = "oldest-policy"

	newestRegex := testutil.NewWatchedNamespaceRegex("newest-regex", `^producao$`)
	newestRegex.CreationTimestamp = metav1.NewTime(time.Unix(200, 0))
	newestRegex.Spec.VhapePolicyRef.Name = "newest-policy"

	tests := []struct {
		name                    string
		watchedNamespaces       []*vhapev1alpha1.VhapeWatchedNamespace
		watchedNamespaceRegexes []*vhapev1alpha1.VhapeWatchedNamespaceRegex
		ignoredNamespaces       []*vhapev1alpha1.VhapeIgnoredNamespace
		ignoredWorkloads        []*vhapev1alpha1.VhapeIgnoredWorkload
		deployment              *appsv1.Deployment
		wantShouldManage        bool
		wantReason              string
		wantConfig              vhapev1alpha1.VhapeWatchedNamespaceSpec
	}{
		{
			name:              "watched namespace manages deployment",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{watchedNamespace},
			deployment:        testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage:  true,
			wantReason:        ReasonWatched,
			wantConfig:        watchedNamespace.Spec,
		},
		{
			name:             "namespace not watched skips deployment",
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonNotWatched,
		},
		{
			name:                    "matching regex manages deployment",
			watchedNamespaceRegexes: []*vhapev1alpha1.VhapeWatchedNamespaceRegex{regex},
			deployment:              testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage:        true,
			wantReason:              ReasonRegexWatched,
			wantConfig:              regex.Spec.VhapeWatchedNamespaceSpec,
		},
		{
			name:                    "oldest matching regex wins",
			watchedNamespaceRegexes: []*vhapev1alpha1.VhapeWatchedNamespaceRegex{newestRegex, oldestRegex},
			deployment:              testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage:        true,
			wantReason:              ReasonRegexWatched,
			wantConfig:              oldestRegex.Spec.VhapeWatchedNamespaceSpec,
		},
		{
			name:                    "watched namespace wins over matching regex",
			watchedNamespaces:       []*vhapev1alpha1.VhapeWatchedNamespace{watchedNamespace},
			watchedNamespaceRegexes: []*vhapev1alpha1.VhapeWatchedNamespaceRegex{regex},
			deployment:              testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage:        true,
			wantReason:              ReasonWatched,
			wantConfig:              watchedNamespace.Spec,
		},
		{
			name:              "ignored namespace wins over watched namespace",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{watchedNamespace},
			ignoredNamespaces: []*vhapev1alpha1.VhapeIgnoredNamespace{testutil.NewIgnoredNamespace(testutil.TestNamespace)},
			deployment:        testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage:  false,
			wantReason:        ReasonNamespaceIgnored,
		},
		{
			name:                    "ignored namespace wins over matching regex",
			watchedNamespaceRegexes: []*vhapev1alpha1.VhapeWatchedNamespaceRegex{regex},
			ignoredNamespaces:       []*vhapev1alpha1.VhapeIgnoredNamespace{testutil.NewIgnoredNamespace(testutil.TestNamespace)},
			deployment:              testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage:        false,
			wantReason:              ReasonNamespaceIgnored,
		},
		{
			name:              "ignored workload wins over ignored namespace",
			ignoredNamespaces: []*vhapev1alpha1.VhapeIgnoredNamespace{testutil.NewIgnoredNamespace(testutil.TestNamespace)},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkload("ignore-api", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonWorkloadIgnored,
		},
		{
			name:              "ignored workload wins over watched namespace",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{watchedNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkload("ignore-api", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: false,
			wantReason:       ReasonWorkloadIgnored,
		},
		{
			name:              "ignored workload with different apiVersion does not match",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{watchedNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", "batch/v1", "Deployment", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantConfig:       watchedNamespace.Spec,
		},
		{
			name:              "ignored workload with different kind does not match",
			watchedNamespaces: []*vhapev1alpha1.VhapeWatchedNamespace{watchedNamespace},
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", appsv1.SchemeGroupVersion.String(), "StatefulSet", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment:       testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantShouldManage: true,
			wantReason:       ReasonWatched,
			wantConfig:       watchedNamespace.Spec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := newScope(
				t,
				nil,
				tt.watchedNamespaces,
				tt.watchedNamespaceRegexes,
				tt.ignoredNamespaces,
				tt.ignoredWorkloads,
			)

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
			if !reflect.DeepEqual(decision.DesiredConfig, tt.wantConfig) {
				t.Fatalf("ShouldManageDeployment().DesiredConfig = %#v, want %#v", decision.DesiredConfig, tt.wantConfig)
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

func TestGetIgnoredWorkload(t *testing.T) {
	tests := []struct {
		name             string
		ignoredWorkloads []*vhapev1alpha1.VhapeIgnoredWorkload
		deployment       *appsv1.Deployment
		wantFound        bool
	}{
		{
			name: "returns true when ignored workload targets deployment",
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkload("ignore-api", testutil.TestNamespace, testutil.TestDeploymentName),
			},
			deployment: testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantFound:  true,
		},
		{
			name:       "returns false when ignored workload list is empty",
			deployment: testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantFound:  false,
		},
		{
			name: "returns false when targetRef is incomplete",
			ignoredWorkloads: []*vhapev1alpha1.VhapeIgnoredWorkload{
				testutil.NewIgnoredWorkloadWithTarget("ignore-api", appsv1.SchemeGroupVersion.String(), "Deployment", "", testutil.TestDeploymentName),
			},
			deployment: testutil.NewDeployment(testutil.TestNamespace, testutil.TestDeploymentName),
			wantFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := newScope(t, nil, nil, nil, nil, tt.ignoredWorkloads)

			got, err := scope.GetIgnoredWorkload(tt.deployment)
			if err != nil {
				t.Fatalf("GetIgnoredWorkload() returned error: %v", err)
			}
			if (got != nil) != tt.wantFound {
				t.Fatalf("GetIgnoredWorkload() found = %v, want %v", got != nil, tt.wantFound)
			}
		})
	}
}

func TestGetIgnoredWorkloadRejectsNilDeployment(t *testing.T) {
	scope := newScope(t, nil, nil, nil, nil, nil)

	_, err := scope.GetIgnoredWorkload(nil)
	if err == nil {
		t.Fatal("GetIgnoredWorkload() expected error, got nil")
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

	_, informers := testutil.NewInformers(
		t,
		nil,
		namespaces,
		watchedNamespaces,
		watchedNamespaceRegexes,
		ignoredNamespaces,
		ignoredWorkloads,
		nil,
	)

	scope, err := New(informers)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	return scope
}

func watchedNamespaceRegexNames(regexes []*vhapev1alpha1.VhapeWatchedNamespaceRegex) []string {
	names := make([]string, 0, len(regexes))
	for _, regex := range regexes {
		if regex != nil {
			names = append(names, regex.Name)
		}
	}
	sort.Strings(names)
	return names
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
