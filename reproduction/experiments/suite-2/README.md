# Suite 2: Tiny Autoscalers vs VHAPE `sma`

## The experiment

**Context.** VHAPE is a fork of the Kubernetes Vertical Pod Autoscaler (VPA) whose recommender runs pluggable
heuristics, selected per resource (CPU, memory) through a `VhapePolicy`. Tiny Autoscalers (Zhao and Uta, *Tiny
Autoscalers for Tiny Workloads: Dynamic CPU Allocation for Serverless Functions*, CCGrid 2022) replaces the recommender
of a 2021 VPA release: CPU is recommended with a simple moving average (`Predict_SMA(3, 2)`), memory keeps the VPA
percentile estimator. This branch adds the `sma` heuristic, a reimplementation of the Tiny CPU recommendation as a VHAPE
heuristic; together with the `vpa` heuristic for memory, `sma-policy` reproduces the whole Tiny recommender.

**Goal.** Show that VHAPE, running `sma-policy`, reproduces the Tiny recommender when the recommendations are applied
to the workloads. If it does, the Tiny behavior is also available in VHAPE, by selecting these heuristics in a policy.

**Question.** Given the same workloads, does VHAPE with `sma-policy` allocate the same resources over time as the Tiny
recommender?

**Hypothesis.** The requests applied to the pods are the same in both environments.

## Design

The experiment runs in two separate and identical clusters (same nodes and Kubernetes version), with the same
workloads, started at the same time.

**Independent variable:** the recommender. Each cluster runs a single one:

| | Tiny environment | VHAPE environment |
|---|---|---|
| Recommender | Tiny `serverless-recommender` (built by [`../../tiny/build.sh`](../../tiny/README.md)) | VHAPE recommender with `sma-policy` (`sma` with `N: 3, input: 2` for CPU, `vpa: {}` for memory) |
| Recommendation objects | created by `create-vpa-objects.sh` | created by the VHAPE watcher |

**Controlled variables:** identical in both environments.

- **Applying recommendations:** updater and admission controller of the upstream VPA 1.6.0, `updateMode: Recreate`
  (the updater evicts a pod whose requests are outside the recommended range; the admission controller sets the
  recommended requests on the new pod).
- **Controlled values:** requests only (`controlledValues: RequestsOnly`); limits stay as in the workload manifests.
- **Recommender interval:** 1s, the default of Tiny (VHAPE is set to 1s through the chart value `extraArgs`).
- **Replicas:** one replica of every component (upstream VPA components, Tiny and VHAPE recommenders).
- **Metrics:** metrics-server with its default `--metric-resolution` of 60s, which the `sma` and `vpa` heuristics expect.
  Both recommenders poll every second but get a new reading once a minute, so the SMA window (`N: 3, input: 2`, six
  readings) covers about six minutes.
- **Workloads:** the same manifests (see [Workloads](#workloads)).

## Workloads

The workloads are not part of this directory. Any set of workloads can be used, as long as:

- they are Deployments (the VHAPE watcher only handles Deployments), and the same manifests, with the same namespaces,
  are applied in both environments;
- their containers define CPU and memory requests (the starting point that the recommendations change);
- their memory usage is above 250Mi, the minimum memory recommendation of Tiny and the VPA; below it, both recommenders
  return the minimum and memory is not really compared;
- their load does not repeat with a period that is a multiple of 1 min; otherwise, with a reading every minute, the
  recommenders always see the same phases of the load.

## Dependent variables

The comparison uses the requests applied to the pods, i.e. the recommendations as they reach the workloads (the
admission controller sets the target recommendation as the request of a pod when the updater evicts it):

- CPU request of the pods, in millicores;
- memory request of the pods, in MiB;
- absolute error between the requests of both environments;
- relative error between the requests of both environments.

The requests of both environments are collected in the same way, every minute (one metrics-server reading), from the
start of the run. The *n*-th sample of a workload in the VHAPE environment is paired with the *n*-th sample of the same
workload in the Tiny environment.

**Absolute error**, computed separately for CPU and memory: for each pair, the absolute difference between the two
requests. Reported per workload and over all workloads: mean absolute error and largest absolute error.

**Relative error**, computed separately for CPU and memory: the absolute error of each pair divided by the request in
the Tiny environment (the reference), as a percentage. Reported per workload and over all workloads: mean relative
error and largest relative error.

## Requirements

- Two clusters as described in [Design](#design), running Kubernetes 1.28 or later (required by VHAPE), each with the
  metrics-server installed and its default `--metric-resolution` of 60s.
- On the machine that runs the commands: `kubectl`, Helm 3, and this repository cloned on the `paper` branch (all
  commands run from the repository root):

  ```bash
  git clone -b paper https://github.com/ufcg-lsd/vhape-recommender.git
  ```

- A Tiny image, in a registry the Tiny cluster can pull from: the published image `<published Tiny image>`, or one built
  with [`../../tiny/build.sh`](../../tiny/README.md#build).
- A VHAPE recommender image with the `sma` and `vpa` heuristics, in a registry the VHAPE cluster can pull from: the
  published image `<published image>`, or one built from this branch as described in
  [`../../vhape/README.md`](../../vhape/README.md#build-the-image).
- The workload manifests (see [Workloads](#workloads)).

## Steps

### Preparation

Add the repository of the upstream VPA chart:

```bash
helm repo add autoscalers https://kubernetes.github.io/autoscaler
```

```bash
helm repo update
```

### Tiny environment

Install the updater, the admission controller and the VerticalPodAutoscaler CRDs of the upstream VPA 1.6.0, one
replica each; the upstream recommender is disabled, Tiny takes its place:

```bash
helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler --version 0.9.0 --namespace kube-system --set recommender.enabled=false --set updater.replicas=1 --set admissionController.replicas=1 --set 'updater.extraArgs={--min-replicas=1}' --wait
```

Create the service account and the roles of the recommender (from the 2021 VPA release Tiny is built on):

```bash
kubectl apply -f reproduction/tiny/deploy/vpa-rbac.yaml
```

Allow it to write the recommendations into the status of the objects (compat patch 3, see
[`../../tiny/PATCHES.md`](../../tiny/PATCHES.md)):

```bash
kubectl apply -f reproduction/tiny/deploy/tiny-status-rbac.yaml
```

Start Tiny, with the image set and without `--vpa-object-namespace`, so it handles the recommendation objects of all
namespaces (it is the only recommender in this cluster):

```bash
sed -e '/--vpa-object-namespace/d' -e 's|image: .*|image: <published Tiny image>|' reproduction/tiny/deploy/tiny-recommender.yaml | kubectl apply -f -
```

Start the script that creates one recommendation object per Deployment of the namespaces that match a regular
expression, like the VHAPE watcher does in the other environment (`'^my-namespace$'` for one namespace, `'^exp-.*$'` for
several). It runs in the background and also handles Deployments created later; it can be stopped once every workload
has its recommendation object (the objects are kept):

```bash
reproduction/experiments/create-vpa-objects.sh '<namespace regex>' &
```

Apply the workloads:

```bash
kubectl apply -f <workload manifests>
```

### VHAPE environment

Install the updater, the admission controller and the VerticalPodAutoscaler CRDs of the upstream VPA 1.6.0, one
replica each; the upstream recommender is disabled:

```bash
helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler --version 0.9.0 --namespace kube-system --set recommender.enabled=false --set updater.replicas=1 --set admissionController.replicas=1 --set 'updater.extraArgs={--min-replicas=1}' --wait
```

Install the VHAPE recommender, with the image that has the `sma` and `vpa` heuristics (one replica, every second, like
Tiny):

```bash
helm upgrade --install vhape-recommender vertical-pod-autoscaler/charts/vhape-recommender --namespace kube-system --set image.repository=<published image repository> --set image.tag=<tag> --set 'extraArgs={--recommender-interval=1s}' --wait
```

Install the VHAPE watcher:

```bash
helm upgrade --install vhape-watcher vertical-pod-autoscaler/charts/vhape-watcher --namespace kube-system --wait
```

Create the policy with the `sma` heuristic for CPU and the `vpa` heuristic for memory:

```bash
kubectl apply -f reproduction/vhape/policies/sma-policy.yaml
```

Tell the watcher which namespaces have workloads, so it creates a recommendation object, with `sma-policy` and
`Recreate`, for every Deployment in them (see the
[watcher guide](../../../vertical-pod-autoscaler/vhape-docs/watcher-guide.md)). Either one `VhapeWatchedNamespace` per
namespace:

```bash
sed "s/CHANGE_ME/<namespace>/" reproduction/experiments/suite-2/watched-namespace.yaml | kubectl apply -f -
```

or one `VhapeWatchedNamespaceRegex` for all of them, with the same regular expression given to the script in the Tiny
environment:

```bash
sed "s/CHANGE_ME/<namespace regex>/" reproduction/experiments/suite-2/watched-namespace-regex.yaml | kubectl apply -f -
```

Apply the same workloads:

```bash
kubectl apply -f <workload manifests>
```

## Between runs

Each run uses new clusters, set up from scratch with the steps above, so no state (recommendation history,
checkpoints, objects) carries over from one run to the next.

## Duration and repetitions

To be defined.
