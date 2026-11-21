# Suite 1: upstream VPA vs VHAPE `vpa`

## The experiment

**Context.** VHAPE is a fork of the Kubernetes Vertical Pod Autoscaler (VPA) whose recommender runs pluggable
heuristics, selected per resource (CPU, memory) through a `VhapePolicy`. The upstream VPA has a single heuristic, fixed
in its recommender and tuned by command-line flags. This branch adds the `vpa` heuristic, a reimplementation of the
upstream VPA 1.6.0 recommender as a VHAPE heuristic.

**Goal.** Show that VHAPE, running the `vpa` heuristic, reproduces the upstream VPA recommender when the
recommendations are applied to the workloads. If it does, the upstream VPA behavior is also available in VHAPE, by
selecting the `vpa` heuristic in a policy.

**Question.** Given the same workloads, does VHAPE with `vpa-policy` allocate the same resources over time as the
upstream VPA recommender?

**Hypothesis.** The requests applied to the pods are the same in both environments.

## Design

The experiment runs in two separate and identical clusters (same nodes and Kubernetes version), with the same
workloads, started at the same time.

**Independent variable:** the recommender. Each cluster runs a single one:

| | Upstream VPA environment | VHAPE environment |
|---|---|---|
| Recommender | upstream VPA recommender 1.6.0 | VHAPE recommender with `vpa-policy` (`vpa: {}` for CPU and memory, i.e. the VPA defaults) |
| Recommendation objects | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vpa` | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vhape` (the object the VHAPE watcher creates) |

**Controlled variables:** identical in both environments.

- **Applying recommendations:** updater and admission controller of the upstream VPA 1.6.0, `updateMode: Recreate`
  (the updater evicts a pod whose requests are outside the recommended range; the admission controller sets the
  recommended requests on the new pod). The objects are created with `updateMode: Off` before the workloads and
  switched to `Recreate` once the initial pods are running, at the same point in both environments (see
  [Recommendation objects](#recommendation-objects)).
- **Controlled values:** requests only (`controlledValues: RequestsOnly`); limits stay as in the workload manifests.
- **Recommender interval:** 1 min, the default of both recommenders.
- **Replicas:** one replica of every component (upstream VPA components and VHAPE recommender).
- **Metrics:** metrics-server with its default `--metric-resolution` of 60s, which the `vpa` heuristic expects.
- **Workloads:** the same manifests (see [Workloads](#workloads)).

## Workloads

The workloads are not part of this directory. Any set of workloads can be used, as long as:

- they are Deployments (the script, like the VHAPE watcher, only handles Deployments), and the same manifests, with the same namespaces,
  are applied in both environments;
- their containers define CPU and memory requests (the starting point that the recommendations change);
- their memory usage is above 250Mi, the minimum memory recommendation of the VPA; below it, both recommenders return
  the minimum and memory is not really compared;
- their load does not repeat with a period that is a multiple of 1 min; otherwise, with a 1 min interval, the
  recommenders always read the same phases of the load.

## Dependent variables

The comparison uses the requests applied to the pods, i.e. the recommendations as they reach the workloads (the
admission controller sets the target recommendation as the request of a pod when the updater evicts it):

- CPU request of the pods, in millicores;
- memory request of the pods, in MiB;
- absolute error between the requests of both environments;
- relative error between the requests of both environments.

The requests of both environments are collected in the same way, every minute (the recommender interval), from the
start of the run. The *n*-th sample of a workload in the VHAPE environment is paired with the *n*-th sample of the same
workload in the upstream VPA environment.

**Absolute error**, computed separately for CPU and memory: for each pair, the absolute difference between the two
requests. Reported per workload and over all workloads: mean absolute error and largest absolute error.

**Relative error**, computed separately for CPU and memory: the absolute error of each pair divided by the request in
the upstream VPA environment (the reference), as a percentage. Reported per workload and over all workloads: mean
relative error and largest relative error.

## Requirements

- Two clusters as described in [Design](#design), running Kubernetes 1.28 or later (required by VHAPE), each with the
  metrics-server installed and its default `--metric-resolution` of 60s.
- On the machine that runs the commands: `kubectl`, Helm 3, and this repository cloned on the `paper` branch (all
  commands run from the repository root):

  ```bash
  git clone -b paper https://github.com/ufcg-lsd/vhape-recommender.git
  ```

- A VHAPE recommender image with the `vpa` heuristic, in a registry the VHAPE cluster can pull from: the published image
  `vtexlsd/vhape-recommender:paper`, or one built from this branch as described in
  [`../../vhape/README.md`](../../vhape/README.md#build-the-image).
- The workload manifests (see [Workloads](#workloads)).

## Steps

### Recommendation objects

In both environments, the objects are created with `updateMode: Off` before the workloads are applied, and switched to
`Recreate` once the initial pods of the workloads are running:

- **Before the workloads**, so both recommenders receive samples from the first instant of every pod. The upstream
  recommender keeps the samples of every pod, with or without an object (`--memory-saver=false`, its default); VHAPE
  feeds only the estimators of existing objects.
- **With `Off`**, so the updater does not evict pods while they are still being placed.
- **Switched to `Recreate`** at the same point in both environments. Changing the update mode does not reset the
  recommendation history of either recommender.

### In both environments

1. Add the repository of the upstream VPA chart:

   ```bash
   helm repo add autoscalers https://kubernetes.github.io/autoscaler
   ```

   ```bash
   helm repo update
   ```

### Upstream VPA environment

1. Install the upstream VPA 1.6.0 (chart `0.9.0`): recommender, updater, admission controller and the
   VerticalPodAutoscaler CRDs, one replica each. `--min-replicas=1` lets the updater act on single-replica workloads:

   ```bash
   helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler --version 0.9.0 --namespace kube-system --set recommender.enabled=true --set recommender.replicas=1 --set updater.replicas=1 --set admissionController.replicas=1 --set 'updater.extraArgs={--min-replicas=1}' --wait
   ```

2. Start the script that creates one recommendation object, with `updateMode: Off` and no `spec.recommenders`, per
   Deployment of every namespace. It runs in the background and also handles Deployments created later:

   ```bash
   reproduction/experiments/suite-1/create-vpa-objects.sh vpa '.*' Off &
   ```

3. Apply the workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

4. Once the initial pods of the workloads are running, at the same point in both environments, stop the script,
   switch every object to `Recreate`, and start the script again with `Recreate`, so Deployments created later get
   `Recreate` objects. It can be stopped once every workload has its recommendation object (the objects are kept):

   ```bash
   kill %1
   ```

   ```bash
   kubectl get vpa -A --no-headers | while read ns name _; do kubectl patch vpa "$name" -n "$ns" --type=merge -p '{"spec":{"updatePolicy":{"updateMode":"Recreate"}}}'; done
   ```

   ```bash
   reproduction/experiments/suite-1/create-vpa-objects.sh vpa '.*' &
   ```

### VHAPE environment

1. Install the updater, the admission controller and the VerticalPodAutoscaler CRDs of the upstream VPA 1.6.0,
   one replica each, with the upstream recommender disabled:

   ```bash
   helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler --version 0.9.0 --namespace kube-system --set recommender.enabled=false --set updater.replicas=1 --set admissionController.replicas=1 --set 'updater.extraArgs={--min-replicas=1}' --wait
   ```

2. Install the VHAPE recommender, with the image that has the `sma` and `vpa` heuristics (one replica, every minute):

   ```bash
   helm upgrade --install vhape-recommender vertical-pod-autoscaler/charts/vhape-recommender --namespace kube-system --set image.tag=paper --wait
   ```

3. Create the policy with the `vpa` heuristic:

   ```bash
   kubectl apply -f reproduction/vhape/policies/vpa-policy.yaml
   ```

4. Start the same script with `vhape` and `Off`: each object gets the annotation `vhape/policy: vpa-policy` and
   `spec.recommenders: [vhape-recommender]`, the object the VHAPE watcher would create. The script is used instead of
   the watcher so that the objects of both environments come from the same code, created at the same pace:

   ```bash
   reproduction/experiments/suite-1/create-vpa-objects.sh vhape '.*' Off &
   ```

5. Apply the same workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

6. At the same point as in the upstream VPA environment, stop the script, switch every object to `Recreate`, and start
   the script again with `Recreate`:

   ```bash
   kill %1
   ```

   ```bash
   kubectl get vpa -A --no-headers | while read ns name _; do kubectl patch vpa "$name" -n "$ns" --type=merge -p '{"spec":{"updatePolicy":{"updateMode":"Recreate"}}}'; done
   ```

   ```bash
   reproduction/experiments/suite-1/create-vpa-objects.sh vhape '.*' &
   ```

## Between runs

Each run uses new clusters, set up from scratch with the steps above, so no state (recommendation history,
checkpoints, objects) carries over from one run to the next.

## Duration and repetitions

To be defined.
