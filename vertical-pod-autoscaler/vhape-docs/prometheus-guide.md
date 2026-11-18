# VHAPE Prometheus

The VHAPE Prometheus chart deploys a dedicated Prometheus instance configured to collect only the metrics required by the VHAPE Recommender.

For installation instructions, see [VHAPE Prometheus installation](prometheus-installation.md).

## Why a dedicated Prometheus

The VHAPE Recommender computes CPU and memory recommendations from time-series data. The dedicated Prometheus instance ensures:

- **Controlled scrape intervals.** The chart scrapes resource metrics at 15s intervals by default. More data points per evaluation window smooths out short-lived spikes and produces more stable recommendations.
- **Guaranteed metric availability.** VHAPE requires specific metrics from the kubelet and kube-state-metrics with specific labels preserved. The chart ships a scrape configuration tailored to these requirements.

## How it works

The chart deploys a single-replica Prometheus with a pre-configured scrape configuration. The VHAPE Recommender connects to this instance via `--prometheus-address`. For the full list of deployed resources, see the [installation guide](prometheus-installation.md).

## Scrape configuration

The chart configures seven scrape jobs. Each job collects a filtered set of metrics using `metric_relabel_configs` to keep only what VHAPE needs.

### Resource metrics (15s interval)

| Job | Metrics path | Collected metrics |
|-----|-------------|-------------------|
| `kubernetes-cadvisor` | `/metrics/resource` | `container_cpu_usage_seconds_total`, `container_memory_working_set_bytes` |
| `cadvisor` | `/metrics/cadvisor` | `container_cpu_cfs_throttled_periods_total`, `container_cpu_cfs_periods_total`, `container_pressure_cpu_waiting_seconds_total`, `container_pressure_memory_waiting_seconds_total` |

These jobs scrape the kubelet on each node at the interval defined by `scrape.resourceInterval` (default: 15s).

### State metrics (30s interval)

| Job | Source | Collected metrics |
|-----|--------|-------------------|
| `kube-state-metrics` | KSM endpoints | Pod ownership, container info, resource requests/limits, restart counts, deployment/statefulset/daemonset/HPA status, node allocatable/conditions/labels |
| `node-exporter` | node-exporter endpoints | `node_cpu_seconds_total`, `node_memory_MemAvailable_bytes`, `node_memory_MemTotal_bytes`, pressure metrics, `node_vmstat_oom_kill` |
| `kubelet-probes` | `/metrics/probes` | `prober_probe_total` |
| `vhape-recommender` | pod discovery | Recommendation metrics (`vhape_recommender_recommendation_*`) exported by the VHAPE Recommender |
| `prometheus` | `localhost:9090` | Prometheus self-monitoring |

These jobs use the interval defined by `scrape.stateInterval` (default: 30s), which is also the global `scrape_interval`.

The recommender itself only queries cAdvisor metrics (`container_*`) and kube-state-metrics (`kube_*`) to compute recommendations. The node-exporter and vhape-recommender jobs are collected for observability dashboards and do not affect recommendations. Disabling the node-exporter subchart does not impact the recommender.

## Subcharts

The chart includes kube-state-metrics and prometheus-node-exporter as optional Helm subcharts. Both are enabled by default but can be disabled if the cluster already runs these components — Prometheus still scrapes existing instances through service discovery.

The kube-state-metrics subchart is configured with `--metric-labels-allowlist=nodes=[*]` to expose node labels, which VHAPE uses for node-level context.

For subchart configuration, see the [installation guide](prometheus-installation.md).

## Storage

The chart creates a PersistentVolumeClaim by default (`storage.enabled: true`). The PVC uses `ReadWriteOnce` access mode.

The deployment strategy switches to `Recreate` when storage is enabled because a `ReadWriteOnce` PVC can only be mounted by one Pod at a time. With the default `RollingUpdate` strategy, Kubernetes tries to start the new Pod before terminating the old one — but the new Pod stays in `Pending` because it cannot mount the PVC while the old Pod still holds it. `Recreate` avoids this by terminating the old Pod first, which releases the PVC so the new Pod can mount it and start normally.

When `retentionSize` is set (default: `40GB`), Prometheus drops the oldest blocks before the disk fills up, preventing write failures. Set `retentionSize` to a value below `storage.size` to leave room for the WAL.

Persistent storage can be disabled with `storage.enabled=false`. In that case the chart uses an `emptyDir` volume — an ephemeral volume allocated on the node that is deleted when the Pod is removed. All TSDB data is lost on restarts, so this is only recommended for testing.

For PVC sizing, storage class configuration, and all available flags, see the [installation guide](prometheus-installation.md).
