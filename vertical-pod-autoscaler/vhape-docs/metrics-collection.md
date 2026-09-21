# Enabling VHAPE metrics collection with kube-state-metrics

This guide explains how to export VHAPE recommendations as Prometheus metrics, so they can be collected by Prometheus, VictoriaMetrics or any other Prometheus-compatible monitoring stack.

The VHAPE Recommender writes its recommendations to the `status.recommendation` field of each `VerticalPodAutoscaler` object. Monitoring systems do not read Kubernetes objects, only metrics, so something has to turn that status into time series. That is the job of [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) (KSM).

Since v2.9, kube-state-metrics no longer exports VPA metrics by default. They have to be declared through its Custom Resource State (CRS) configuration. The `vhape-kube-state-metrics` chart packages that configuration together with a **dedicated** KSM instance, so the cluster's own kube-state-metrics does not need to be modified.

## Prerequisites

You need:

- the upstream VPA CRDs installed, as described in [VHAPE Recommender installation](recommender-installation.md);
- the VHAPE Recommender installed;
- a Prometheus-compatible collector running in the cluster (for example Prometheus or `vmagent`).

## Install the chart

The Helm chart installs:

- a ServiceAccount;
- a ClusterRole and ClusterRoleBinding granting `list` and `watch` on `verticalpodautoscalers` and `customresourcedefinitions` only;
- a ConfigMap holding the CRS configuration;
- a kube-state-metrics Deployment running with `--custom-resource-state-only`;
- a Service exposing the metrics on port `8080`.

Install the published OCI chart:

```bash
helm upgrade --install vhape-kube-state-metrics \
  oci://registry-1.docker.io/vtexlsd/vhape-kube-state-metrics-chart \
  --namespace kube-system \
  --wait
```

Check that the Pod is running:

```bash
kubectl get pods -n kube-system -l app=vhape-kube-state-metrics
```

Because it runs with `--custom-resource-state-only`, this instance exports **only** the VPA recommendation metrics below. It never exports `kube_pod_*`, `kube_deployment_*` or any other series from the cluster's own kube-state-metrics, so both instances can run side by side without duplicating data.

## Exported metrics

| Metric | Source field |
|---|---|
| `kube_customresource_vpa_containerrecommendations_target` | `status.recommendation.containerRecommendations[].target` |
| `kube_customresource_vpa_containerrecommendations_lowerbound` | `status.recommendation.containerRecommendations[].lowerBound` |
| `kube_customresource_vpa_containerrecommendations_upperbound` | `status.recommendation.containerRecommendations[].upperBound` |

Each metric is exported twice per container, once per resource, and carries the following labels:

| Label | Value |
|---|---|
| `resource` | `cpu` or `memory` |
| `unit` | `core` for CPU, `byte` for memory |
| `namespace` | namespace of the VPA object |
| `verticalpodautoscaler` | name of the VPA object |
| `target_api_version`, `target_kind`, `target_name` | the workload referenced by `spec.targetRef` |
| `container` | container name |

## Configure scraping

The chart's Service is annotated with:

```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "8080"
```

If your collector discovers targets through these annotations, which is the common `kubernetes-service-endpoints` job in Prometheus and `vmagent` configurations, no further setup is needed.

If your collector discovers targets through the Prometheus Operator instead, create a `ServiceMonitor` for the Service:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: vhape-kube-state-metrics
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: vhape-kube-state-metrics
  endpoints:
    - port: http-metrics
      interval: 60s
```

The VictoriaMetrics Operator converts `ServiceMonitor` objects into `VMServiceScrape` automatically, so the same manifest works there.

## Verify

Check that the metrics are exposed:

```bash
kubectl port-forward -n kube-system svc/vhape-kube-state-metrics 8080:8080 >/dev/null 2>&1 & PF=$!
sleep 3
curl -s localhost:8080/metrics | grep kube_customresource_vpa_containerrecommendations
kill $PF
```

Then check that your collector is scraping it:

```promql
up{service="vhape-kube-state-metrics"}
```

A value of `1` means the last scrape succeeded.

Two behaviors are expected and do not indicate a problem:

- **No series at all** while no VPA has a recommendation yet. The metrics are generated from `status.recommendation`, so they only appear once the VHAPE Recommender has written one.
- **`target`, `lowerbound` and `upperbound` with identical values** right after a VPA is created. Until the policy's `minimumCoverageWindow` is reached, the recommender falls back to the container's current request for all three fields. They diverge once enough samples are collected. See [Recommendations stay equal to current requests](recommender-guide.md#recommendations-stay-equal-to-current-requests).

## Configuration

The main chart values are:

| Value | Default | Description |
|---|---|---|
| `image.repository` | `registry.k8s.io/kube-state-metrics/kube-state-metrics` | kube-state-metrics image. |
| `image.tag` | `""` | Empty tag falls back to `v<appVersion>` (`v2.20.0`). |
| `resources` | requests `100m` / `256Mi`, memory limit `300Mi` | Container resources. There is no CPU limit. |
| `service.annotations` | `prometheus.io/scrape` and `prometheus.io/port` | Annotations used for scrape discovery. |
| `crsConfig` | VPA target, lower bound and upper bound | The Custom Resource State configuration. |

Changing `crsConfig` restarts the Pod automatically, because its checksum is stored as a Pod annotation.

If you override the image, keep kube-state-metrics at **v2.9 or later**. Earlier versions label custom resource metrics differently, and the metric names above would change.

## Operational notes

### The `job` label differs from the cluster's kube-state-metrics

These series are scraped from a separate target, so their `job` label is not the one used by the cluster's own kube-state-metrics. With annotation-based discovery, they usually arrive as `job="kubernetes-service-endpoints"` and `service="vhape-kube-state-metrics"`. Dashboards and alerts that filter on `job="kube-state-metrics"` need to be updated.

### Keep the release outside namespaces managed by the VHAPE Watcher

If the VHAPE Watcher is installed with a broad `VhapeWatchedNamespaceRegex`, it creates a VPA for every Deployment in the matching namespaces, including this one. Install the chart in `kube-system`, which the watcher chart ignores by default, or create a `VhapeIgnoredNamespace` for the namespace you choose. See the [VHAPE Watcher guide](watcher-guide.md).

## Troubleshooting

### The Pod is not ready

Check the logs:

```bash
kubectl logs -n kube-system deploy/vhape-kube-state-metrics
```

An invalid CRS configuration is reported at startup. A `forbidden` error when listing resources means the ClusterRole or ClusterRoleBinding is missing.

### The collector does not show the target

Confirm the annotations are present on the Service:

```bash
kubectl get svc -n kube-system vhape-kube-state-metrics -o jsonpath='{.metadata.annotations}'
```

If they are, check whether your collector discovers targets through annotations at all. If it uses the Prometheus Operator, create the `ServiceMonitor` shown in [Configure scraping](#configure-scraping).
