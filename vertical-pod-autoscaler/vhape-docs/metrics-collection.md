# Enabling VHAPE metrics collection

This guide explains how to export VHAPE recommendations as Prometheus metrics, so they can be collected by Prometheus, VictoriaMetrics or any other Prometheus-compatible monitoring stack.

The VHAPE Recommender serves its recommendations on its own `/metrics` endpoint, next to the metrics inherited from the upstream VPA recommender. No extra component is needed.

## Prerequisites

You need:

- the VHAPE Recommender installed, as described in [VHAPE Recommender installation](recommender-installation.md);
- a Prometheus-compatible collector running in the cluster (for example Prometheus or `vmagent`).

## Exported metrics

| Metric | Source field |
|---|---|
| `vhape_recommender_recommendation_target` | `status.recommendation.containerRecommendations[].target` |
| `vhape_recommender_recommendation_lowerbound` | `status.recommendation.containerRecommendations[].lowerBound` |
| `vhape_recommender_recommendation_upperbound` | `status.recommendation.containerRecommendations[].upperBound` |

The values are recorded after post-processing, so they match what is written to the VPA status.

Each metric is exported twice per container, once per resource, with the following labels:

| Label | Value |
|---|---|
| `resource` | `cpu` or `memory` |
| `unit` | `core` for CPU, `byte` for memory |
| `namespace` | namespace of the VPA object |
| `verticalpodautoscaler` | name of the VPA object |
| `target_api_version`, `target_kind`, `target_name` | the workload referenced by `spec.targetRef` |
| `container` | container name |

Values are in base units: cores for CPU and bytes for memory, the same units used by kube-state-metrics for requests and usage.

Your collector adds its own labels on top of these, such as `job` and `instance`. With annotation-based discovery, the `namespace` of the series is the namespace of the VPA, while the namespace of the recommender Pod usually arrives as `kubernetes_namespace`.

## Configure scraping

The recommender chart controls the endpoint through two values:

| Value | Default | Description |
|---|---|---|
| `metrics.port` | `8942` | Port of the `/metrics` endpoint. |
| `metrics.scrape` | `true` | Adds the `prometheus.io/*` annotations to the Pod. |

With `metrics.scrape` enabled, collectors that discover Pods through annotations, such as the usual `kubernetes-pods` job in Prometheus and `vmagent`, pick the recommender up with no extra setup.

With the Prometheus Operator, create a `PodMonitor` instead. The VictoriaMetrics Operator converts it into a `VMPodScrape` automatically.

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: vhape-recommender
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: vhape-recommender
  podMetricsEndpoints:
    - port: metrics
      interval: 60s
```

## Verify

Check that the metrics are exposed:

```bash
kubectl port-forward -n kube-system deploy/vhape-recommender 8942:8942 >/dev/null 2>&1 & PF=$!
sleep 3
curl -s localhost:8942/metrics | grep vhape_recommender_recommendation
kill $PF
```

Then check that your collector is scraping it. With the `kubernetes-pods` job, the Pod labels become series labels:

```promql
up{app="vhape-recommender"}
```

A value of `1` means the last scrape succeeded.

Two behaviors are expected and do not indicate a problem:

- **No series at all** while no VPA has a recommendation yet.
- **`target`, `lowerbound` and `upperbound` with identical values** right after a VPA is created. Until the policy's `minimumCoverageWindow` is reached, the recommender falls back to the container's current request for all three fields. They diverge once enough samples are collected. See [Recommendations stay equal to current requests](recommender-guide.md#recommendations-stay-equal-to-current-requests).

## Troubleshooting

### The collector does not show the target

Confirm the discovery annotations are present on the Pod:

```bash
kubectl get pods -n kube-system -l app=vhape-recommender -o jsonpath='{.items[0].metadata.annotations}'
```

If they are missing, check that `metrics.scrape` is enabled in the chart values. If they are present, check whether your collector discovers Pods through annotations at all. If it uses the Prometheus Operator, create the `PodMonitor` shown in [Configure scraping](#configure-scraping).
