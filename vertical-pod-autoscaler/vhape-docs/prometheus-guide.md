# VHAPE Prometheus

The VHAPE Prometheus chart deploys a dedicated Prometheus instance configured to collect only the metrics required by the VHAPE Recommender. It replaces the need for a shared, cluster-wide Prometheus for VHAPE workloads.

## Why a dedicated Prometheus

The VHAPE Recommender computes CPU and memory recommendations from time-series data. Using a shared Prometheus introduces two problems:

- **Scrape interval mismatch.** Shared instances typically scrape at 30s or 60s intervals. VHAPE benefits from 15s intervals on resource metrics to reduce recommendation drift.
- **Metric availability.** VHAPE requires specific metrics from the kubelet and kube-state-metrics. A shared Prometheus may not collect all of them, or may apply relabeling that drops required labels.

The dedicated instance solves both by shipping a scrape configuration tailored to VHAPE.

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

The chart includes kube-state-metrics and prometheus-node-exporter as optional Helm dependencies. Both are enabled by default.

Disable them if the cluster already runs these components:

```bash
--set kube-state-metrics.enabled=false
--set prometheus-node-exporter.enabled=false
```

When disabled, Prometheus still scrapes the existing KSM and node-exporter instances through service discovery. The `serviceDiscoveryNamespaces` value controls which namespaces are searched (default: `kube-system` and `monitoring`).

The kube-state-metrics subchart is configured with `--metric-labels-allowlist=nodes=[*]` to expose node labels, which VHAPE uses for node-level context.

## Storage

The chart creates a PersistentVolumeClaim by default (`storage.enabled: true`). The PVC uses `ReadWriteOnce` access mode.

The deployment strategy switches to `Recreate` when storage is enabled to avoid PVC mount conflicts during rolling updates.

When `retentionSize` is set (default: `40GB`), Prometheus drops the oldest blocks before the disk fills up, preventing write failures. Set `retentionSize` to a value below `storage.size` to leave room for the WAL.

Configure the PVC size at install time:

```bash
--set storage.size=100Gi
--set retentionSize=80GB
```

To use a specific storage class:

```bash
--set storage.storageClassName=gp3
```

To disable persistent storage and use `emptyDir` instead:

```bash
--set storage.enabled=false
```

For installation instructions, see [VHAPE Prometheus installation](prometheus-installation.md).
