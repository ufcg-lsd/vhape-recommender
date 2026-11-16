# VHAPE Prometheus installation

This guide explains how to install the dedicated Prometheus instance used by the VHAPE Recommender.

## Prerequisites

You need:

- a Kubernetes cluster running version 1.28.0 or later;
- `kubectl` configured to access the cluster;
- Helm installed locally;
- the VHAPE Recommender installed (see [VHAPE Recommender installation](recommender-installation.md)).

## Install the VHAPE Prometheus chart

The Helm chart installs:

- a Prometheus Deployment with a filtered scrape configuration;
- a Service named `vhape-prometheus` on port 9090;
- RBAC resources (ClusterRole, ClusterRoleBinding, ServiceAccount);
- a PersistentVolumeClaim for TSDB storage (enabled by default);
- optionally, kube-state-metrics and prometheus-node-exporter as subcharts.

Install the published OCI chart:

```bash
helm upgrade --install vhape-prometheus \
  oci://registry-1.docker.io/vtexlsd/vhape-prometheus \
  --namespace monitoring \
  --create-namespace \
  --wait
```

### Configure the VHAPE Recommender address

The VHAPE Recommender must be configured to query this Prometheus instance. Set the `--prometheus-address` flag on the recommender to point to the Service created by this chart:

```bash
helm upgrade --install vhape-recommender \
  oci://registry-1.docker.io/vtexlsd/vhape-recommender-chart \
  --namespace kube-system \
  --set extraArgs.prometheus-address=http://vhape-prometheus.monitoring.svc:9090 \
  --wait
```

### Customizing the installation

Override values at install time with `--set`:

```bash
helm upgrade --install vhape-prometheus \
  oci://registry-1.docker.io/vtexlsd/vhape-prometheus \
  --namespace monitoring \
  --create-namespace \
  --set storage.size=50Gi \
  --set retention=30d \
  --set kube-state-metrics.enabled=false \
  --wait
```

Available flags:

| Flag | Default | Description |
|------|---------|-------------|
| `image.repository` | `prom/prometheus` | Prometheus container image |
| `image.tag` | Chart `appVersion` | Image tag |
| `retention` | `15d` | TSDB retention period |
| `storage.enabled` | `true` | Create a PersistentVolumeClaim for TSDB data |
| `storage.size` | `10Gi` | PVC size |
| `storage.storageClassName` | (cluster default) | Storage class for the PVC |
| `scrape.resourceInterval` | `15s` | Scrape interval for CPU/memory metrics |
| `scrape.stateInterval` | `30s` | Scrape interval for state metrics (KSM, node-exporter) |
| `serviceDiscoveryNamespaces` | `[kube-system, monitoring]` | Namespaces where KSM and node-exporter are discovered |
| `kube-state-metrics.enabled` | `true` | Deploy kube-state-metrics as a subchart |
| `prometheus-node-exporter.enabled` | `true` | Deploy node-exporter as a subchart |
| `resources.requests.cpu` | `250m` | CPU request |
| `resources.requests.memory` | `256Mi` | Memory request |
| `resources.limits.cpu` | `1` | CPU limit |
| `resources.limits.memory` | `1Gi` | Memory limit |

Disable the subcharts if the cluster already runs kube-state-metrics or node-exporter:

```bash
helm upgrade --install vhape-prometheus \
  oci://registry-1.docker.io/vtexlsd/vhape-prometheus \
  --namespace monitoring \
  --create-namespace \
  --set kube-state-metrics.enabled=false \
  --set prometheus-node-exporter.enabled=false \
  --wait
```

## Verify the installation

Check that the Prometheus Pod is running:

```bash
kubectl get pods -n monitoring -l app.kubernetes.io/name=vhape-prometheus
```

Check logs:

```bash
kubectl logs -n monitoring deploy/vhape-prometheus
```

Port-forward to verify targets:

```bash
kubectl port-forward -n monitoring svc/vhape-prometheus 9090:9090
```

Open `http://localhost:9090/targets` in a browser. All targets should show as UP.

## ArgoCD

To deploy the chart through ArgoCD, create an `Application` resource:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: vhape-prometheus
  namespace: argocd
spec:
  project: default
  source:
    chart: vhape-prometheus
    repoURL: registry-1.docker.io/vtexlsd
    targetRevision: 0.1.0
    helm:
      valuesObject:
        storage:
          size: 50Gi
  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

## Uninstall

```bash
helm uninstall vhape-prometheus -n monitoring
```

The PVC is not deleted automatically. To remove it:

```bash
kubectl delete pvc -n monitoring -l app.kubernetes.io/name=vhape-prometheus
```

## Next steps

See the [VHAPE Prometheus guide](prometheus-guide.md) for details on the scrape configuration and collected metrics.
