# Release guide

This guide explains how to build and publish VHAPE container images and Helm charts.

The release process is the same for the VHAPE Recommender and VHAPE Watcher. The only difference is the directory used when building the image and packaging the Helm chart.

## Building a container image

Both components are under `vertical-pod-autoscaler/pkg`:

- `recommender` for the VHAPE Recommender;
- `vhapewatcher` for the VHAPE Watcher.

From the component directory, build and publish the image:

```bash
make release REGISTRY=<registry> TAG=<tag> ALL_ARCHITECTURES=amd64
```

## Publishing a Helm chart

Use this section when you need to package and publish a new VHAPE Helm chart version.

### Step 1. Update the chart metadata

Before publishing a new chart, update its `Chart.yaml`.

Both charts are under `vertical-pod-autoscaler/charts`:

- `vhape-recommender` for the VHAPE Recommender;
- `vhape-watcher` for the VHAPE Watcher.

Update the chart version and application version as needed:

```yaml
version: 0.1.0
appVersion: "1.0.0"
```

- `version` is the Helm chart version.
- `appVersion` is the component application version.

If only the chart changed, increment `version`:

```yaml
version: 0.1.1
appVersion: "1.0.0"
```

If the component image also changed, increment both `version` and `appVersion`:

```yaml
version: 0.2.0
appVersion: "1.1.0"
```

Make sure `values.yaml` points to the intended image version. If `values.yaml` leaves `image.tag` empty, the chart uses `appVersion` as the image tag.

Do not change the spec of a default policy while keeping its existing name. `VhapePolicy.spec` is immutable, so upgrades of installations that already contain that policy would fail.

### Step 2. Package the chart

Package the chart:

```bash
helm package vertical-pod-autoscaler/charts/<chart>
```

### Step 3. Log in to an OCI registry

Log in to the OCI registry where the chart will be published:

```bash
helm registry login <registry-host> -u <username>
```

For example, for Docker Hub:

```bash
helm registry login registry-1.docker.io -u <dockerhub-username>
```

### Step 4. Push the chart to the OCI registry

Push the packaged chart:

```bash
helm push <chart-name>-<chart-version>.tgz \
  oci://<registry-host>/<registry-path>
```

For Docker Hub:

```bash
helm push <chart-name>-<chart-version>.tgz \
  oci://registry-1.docker.io/<dockerhub-username>
```
