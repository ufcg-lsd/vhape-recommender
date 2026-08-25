# VHAPE Watcher

The VHAPE Watcher is an optional component used with the VHAPE Recommender. It automates the association between Deployments and the recommender by creating and maintaining `VerticalPodAutoscaler` objects.

Without the watcher, VPA objects must be created and managed separately for each workload, including the VHAPE Recommender and the `VhapePolicy` that should be used, as described in the [VHAPE Recommender guide](recommender-guide.md).

The watcher runs as a Deployment in the Kubernetes cluster and is configured through cluster-scoped custom resources:

* `VhapeWatchedNamespace`, which configures automatic VPA management for a specific namespace;
* `VhapeWatchedNamespaceRegex`, which configures automatic VPA management for namespaces matching a regular expression;
* `VhapeIgnoredNamespace`, which excludes a namespace from automatic management;
* `VhapeIgnoredWorkload`, which excludes a specific Deployment from automatic management.

For installation instructions, see [VHAPE Watcher installation](watcher-installation.md).

## How it works

The VHAPE Watcher continuously reconciles Deployments, VPAs, and watcher configuration resources.

For each Deployment, the watcher resolves its scope and configuration in the following order:

```text
Deployment
├── Manually configured VPA
│   └── Manual VPA is preserved
├── VhapeIgnoredWorkload
│   └── Deployment is not managed
├── VhapeIgnoredNamespace
│   └── Deployment is not managed
├── VhapeWatchedNamespace
│   └── Namespace-specific configuration is used
├── VhapeWatchedNamespaceRegex
│   └── Matching regex configuration is used
└── No matching configuration
    └── Deployment is not managed
```

A manual VPA provides workload-specific configuration. `VhapeWatchedNamespace` provides namespace-specific configuration. `VhapeWatchedNamespaceRegex` provides a shared default for multiple namespaces. If multiple `VhapeWatchedNamespaceRegex` resources match the same namespace, the oldest matching resource is used.

When a deployment is not managed by Vhape Watcher or a manual VPA is present, any previously automatically created VPAs are thus deleted.

Creating, updating or deleting any resource listed above causes affected deployments to be reconciled again.


## Watch namespaces by regex

Create a `VhapeWatchedNamespaceRegex` to apply the same watcher configuration to multiple namespaces.

Example at `vertical-pod-autoscaler/pkg/vhapewatcher/yamls/vhapewatchednamespaceregex_example.yaml`:

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapeWatchedNamespaceRegex
metadata:
  name: production-namespaces # Name of this namespace matching rule.
spec:
  # Regular expression used to select namespaces.
  # This example matches namespaces starting with "prod-".
  regex: "^prod-.*$"
  vhapePolicyName: p93-percentile-hysteresis # VhapePolicy used by managed VPAs
  vpaUpdateMode: InPlaceOrRecreate # update mode used by managed VPAs
```

## Watch a specific namespace

Create a `VhapeWatchedNamespace` to manage a specific namespace. This resource is cluster-scoped, and its `metadata.name` identifies the namespace.

Example at `vertical-pod-autoscaler/pkg/vhapewatcher/yamls/vhapewatchednamespace_example.yaml`:

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapeWatchedNamespace
metadata:
  name: production # namespace to be managed by the VHAPE Watcher
spec:
  vhapePolicyName: p93-percentile-hysteresis # VhapePolicy used by watcher-managed VPAs in this namespace
  vpaUpdateMode: InPlaceOrRecreate # update mode used by watcher-managed VPAs in this namespace
```

A `VhapeWatchedNamespace` takes precedence over any `VhapeWatchedNamespaceRegex` matching the same namespace.

## Ignore a namespace

Create a `VhapeIgnoredNamespace` to exclude a namespace from automatic watcher management. This is especially useful for excluding individual namespaces selected by a broad regex rule.

The resource is cluster-scoped, and its `metadata.name` identifies the namespace to ignore.

Example at `vertical-pod-autoscaler/pkg/vhapewatcher/yamls/vhapeignorednamespace_example.yaml`:

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapeIgnoredNamespace
metadata:
  name: kube-system # Namespace to be ignored by the VHAPE Watcher.
```

## Ignore a workload

Create a `VhapeIgnoredWorkload` when a Deployment should remain outside automatic watcher management.

Example at `vertical-pod-autoscaler/pkg/vhapewatcher/yamls/vhapeignoredworkload_example.yaml`:

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapeIgnoredWorkload
metadata:
  name: legacy-api-deployment-production # name of the exclusion rule
spec:
  targetRef: # Deployment to be excluded from Watcher management
    apiVersion: apps/v1
    kind: Deployment
    namespace: production # namespace where the Deployment is defined
    name: legacy-api # name of the Deployment
  reason: "VPA managed manually" # optional reason for excluding the workload
```

## Manual VPA configuration

A manually managed VPA targeting a Deployment has the highest configuration precedence. The watcher preserves the manual VPA and removes any watcher-generated VPA targeting the same Deployment.

For more information about configuring VPA objects, see the [VHAPE Recommender guide](recommender-guide.md).

## VPA ownership

The watcher identifies generated VPAs through the label:

```yaml
app.kubernetes.io/managed-by: vhape-watcher
```

VPAs without this label are treated as manually managed. The label should therefore be reserved for watcher-generated VPAs.

Watcher-generated VPAs are continuously reconciled against the selected configuration and should not be edited. Direct changes to their specification may cause them to be deleted and recreated. For workload-specific configuration, a VPA object should be created manually targeting that workload.
