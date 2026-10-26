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

The VHAPE Watcher continuously reconciles Deployments, VPAs, HPAs, and watcher configuration resources.

For each Deployment, the watcher resolves automatic VPA management in the following order:

```text
Deployment
├── Manually configured VPA
│   └── Preserve it and remove any watcher-generated VPA
├── VhapeIgnoredWorkload
│   └── Automatic VPA management is disabled
├── VhapeIgnoredNamespace
│   └── Automatic VPA management is disabled
├── VhapeWatchedNamespace
│   └── Namespace-specific configuration is used
├── VhapeWatchedNamespaceRegex
│   └── Matching regex configuration is used
└── No matching configuration
    └── Automatic VPA management is disabled
```

A manual VPA provides workload-specific configuration. `VhapeWatchedNamespace` provides namespace-specific configuration. `VhapeWatchedNamespaceRegex` provides a shared default for multiple namespaces. If multiple `VhapeWatchedNamespaceRegex` resources match the same namespace, the oldest matching resource is used.

When automatic management is disabled or a manual VPA is present, any previously automatically created VPAs are deleted.

Changes to Deployments, VPAs, or watcher configuration resources cause affected Deployments to be reconciled again. HPA-specific event behavior is described below.

## HPA management

HPA management is selected from VPAs, independently from automatic VPA management. The watcher considers VPAs that target the reconciled Deployment and contain both:

```yaml
metadata:
  labels:
    autoscaling.vhape.io/recommender: <vhape-recommender-name>
  annotations:
    vhape/policy: <vhape-policy-name>
```

It chooses the eligible VPA with the greatest `metadata.creationTimestamp`. If timestamps are equal, it chooses the lexicographically greatest VPA name. It then retrieves the `VhapePolicy` named by `vhape/policy` and reads `spec.manageHpa`:

```yaml
spec:
  manageHpa: true
```

When the selected policy is absent or `manageHpa` is `false`, the watcher leaves HPAs unchanged. When it is `true`, the watcher finds HPAs in the same namespace whose `scaleTargetRef` is the reconciled Deployment. Having no matching HPA is valid and requires no action.

For matching HPAs, the watcher converts only `Resource` and `ContainerResource` metrics whose target type is `Utilization`. Targets already using `AverageValue` and all other metric types remain unchanged.

The conversion uses the request currently declared in the Deployment's Pod template:

```text
averageValue = max(1m, floor(request * averageUtilization / 100))
```

For example, a CPU target of `75%` and a total CPU request of `400m` become an `AverageValue` target of `300m`. Values smaller than one milli-unit are rounded up to `1m`. The watcher updates the HPA target, clears `averageUtilization`, and records the percentage used for the conversion in the `autoscaling.vhape.io/original-hpa-utilization` annotation:

```yaml
metadata:
  annotations:
    autoscaling.vhape.io/original-hpa-utilization: '{"resource/cpu":75,"container/api/cpu":60}'
```

The map key identifies either a Pod-level resource metric (`resource/<resource>`) or a container resource metric (`container/<container>/<resource>`).

The conversion is one-way: setting `manageHpa` to `false`, changing Deployment requests, or deleting the watcher does not restore an HPA target to `Utilization`.

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

Create a `VhapeIgnoredNamespace` to exclude a namespace from automatic watcher management. This is especially useful for excluding individual namespaces selected by a broad regex rule. It does not directly prevent HPA management selected by an eligible VPA.

The resource is cluster-scoped, and its `metadata.name` identifies the namespace to ignore.

Example at `vertical-pod-autoscaler/pkg/vhapewatcher/yamls/vhapeignorednamespace_example.yaml`:

```yaml
apiVersion: autoscaling.vhape.io/v1alpha1
kind: VhapeIgnoredNamespace
metadata:
  name: kube-system # Namespace to be ignored by the VHAPE Watcher.
```

## Ignore a workload

Create a `VhapeIgnoredWorkload` when a Deployment should remain outside automatic watcher management. It does not directly prevent HPA management selected by an eligible VPA.

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

A manually managed VPA targeting a Deployment has the highest configuration precedence for VPA lifecycle. The watcher preserves the manual VPA and removes any watcher-generated VPA targeting the same Deployment.

A manually managed VPA must always carry the `autoscaling.vhape.io/recommender` label. This identifies it as a VPA served by a VHAPE recommender, as explained in the [VHAPE Recommender guide](recommender-guide.md).

## VPA ownership

The watcher identifies generated VPAs through the label:

```yaml
app.kubernetes.io/managed-by: vhape-watcher
```

VPAs without this label are treated as manually managed. The label should therefore be reserved for watcher-generated VPAs.

Watcher-generated VPAs are continuously reconciled against the selected configuration and should not be edited. Direct changes to their specification may cause them to be deleted and recreated. For workload-specific configuration, a VPA object should be created manually targeting that workload.
