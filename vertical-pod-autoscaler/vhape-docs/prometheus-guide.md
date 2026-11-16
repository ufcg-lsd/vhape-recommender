# VHAPE Prometheus

The VHAPE Prometheus chart deploys a dedicated Prometheus instance configured to collect only the metrics required by the VHAPE Recommender. It replaces the need for a shared, cluster-wide Prometheus for VHAPE workloads.

## Why a dedicated Prometheus

The VHAPE Recommender computes CPU and memory recommendations from time-series data. Using a shared Prometheus introduces two problems:

- **Scrape interval mismatch.** Shared instances typically scrape at 30s or 60s intervals. VHAPE benefits from 15s intervals on resource metrics to reduce recommendation drift.
- **Metric availability.** VHAPE requires specific metrics from the kubelet, kube-state-metrics, and node-exporter. A shared Prometheus may not collect all of them, or may apply relabeling that drops required labels.

The dedicated instance solves both by shipping a scrape configuration tailored to VHAPE.

## Scrape configuration

The chart configures six scrape jobs. Each job collects a filtered set of metrics using `metric_relabel_configs` to keep only what VHAPE needs.

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
| `prometheus` | `localhost:9090` | Prometheus self-monitoring |

These jobs use the interval defined by `scrape.stateInterval` (default: 30s), which is also the global `scrape_interval`.

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

Configure the PVC size at install time:

```bash
--set storage.size=50Gi
```

To use a specific storage class:

```bash
--set storage.storageClassName=gp3
```

To disable persistent storage and use `emptyDir` instead:

```bash
--set storage.enabled=false
```

## Security

The Prometheus container runs as user `nobody` (UID 65534) with `runAsNonRoot: true`. The ClusterRole grants read-only access to nodes, node metrics, services, endpoints, and pods.

## Operational notes

### Configuration changes restart the Pod

The Deployment includes a `checksum/config` annotation derived from the ConfigMap. When the scrape configuration changes, the annotation changes, and Kubernetes rolls out a new Pod automatically.

### VHAPE Recommender connection

The VHAPE Recommender must be configured with `--prometheus-address` pointing to the Service created by this chart:

```
http://vhape-prometheus.monitoring.svc:9090
```

The recommender also requires `--prometheus-cadvisor-job-name=kubernetes-cadvisor`, which matches the job name hardcoded in this chart's scrape configuration.

For installation instructions, see [VHAPE Prometheus installation](prometheus-installation.md).
