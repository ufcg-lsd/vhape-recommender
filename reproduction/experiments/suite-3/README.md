# Suite 3: Tiny and upstream VPA together vs VHAPE with both heuristics

## The experiment

**Context.** Suites 1 and 2 compare each VHAPE heuristic with the recommender it reproduces, one at a time: every
workload of a cluster uses the same heuristic. VHAPE, however, selects the heuristic per workload, through the policy
of its recommendation object, so a single VHAPE recommender can serve workloads with different heuristics at the same
time. Without VHAPE, the same setup needs one recommender per heuristic, running side by side in the cluster.

**Goal.** Show that a single VHAPE recommender, running `sma` for some workloads and `vpa` for the others, gives the same
recommendations as Tiny and the upstream VPA recommender running together, each one handling its own workloads.

**Question.** Given the same workloads, split in the same way between the two heuristics, does VHAPE recommend the same
resources over time as Tiny and the upstream VPA together?

**Hypothesis.** The target recommendation of every workload is the same in both environments.

## Design

The experiment runs in two separate and identical clusters (same nodes and Kubernetes version), with the same
workloads, started at the same time.

**Independent variable:** the recommenders. Each workload is assigned to a heuristic by its name: the Deployments
listed in [`w2c-workloads.txt`](w2c-workloads.txt) (one `namespace/name` per line) go to the SMA, every other
Deployment goes to the VPA.

| | Tiny + VPA environment | VHAPE environment |
|---|---|---|
| Recommenders | Tiny `serverless-recommender` for the listed Deployments, upstream VPA recommender 1.6.0 for the others | one VHAPE recommender: `sma-policy` for the listed Deployments, `vpa-policy` for the others |
| Recommendation objects | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `tiny-vpa` | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vhape` (the object the VHAPE watcher creates) |

In the Tiny + VPA environment, each object is marked so that only one of the two recommenders handles it:

- **listed Deployments:** `spec.recommenders: [tiny]`. The upstream VPA skips objects that do not list its name; Tiny
  ignores the field (the 2021 VPA API it is built on does not have it).
- **other Deployments:** the annotation `vhape/policy: vpa-policy`. Tiny skips objects with this annotation (compat
  patch, see [`../../tiny/PATCHES.md`](../../tiny/PATCHES.md)); the upstream VPA ignores it.

In the VHAPE environment, the script is used instead of the VHAPE watcher because the watcher assigns one policy per
namespace, and the namespaces of these workloads mix listed and unlisted Deployments.

**Controlled variables:** identical in both environments.

- **Applying recommendations:** none, `updateMode: Off`, as in suite 2. The updater and the admission controller of the
  upstream VPA 1.6.0 are installed but do nothing with `Off` objects, so both environments run the same pods.
- **Controlled values:** requests only (`controlledValues: RequestsOnly`); with `Off` it has no effect.
- **Recommender interval:** 1 min for every recommender. This is the default of the upstream VPA and of VHAPE; Tiny is
  set to 1 min in its manifest ([`tiny-recommender-suite-3.yaml`](../../tiny/deploy/tiny-recommender-suite-3.yaml)),
  instead of its default of 1s, so that all recommenders run at the same interval.
- **Replicas:** one replica of every component.
- **Metrics:** metrics-server with its default `--metric-resolution` of 60s.
- **Workloads:** the same manifests (see [Workloads](#workloads)).

## Workloads

The workloads are not part of this directory. They must meet the requirements of suites 1 and 2:

- they are Deployments, and the same manifests, with the same namespaces, are applied in both environments;
- their containers define CPU and memory requests;
- their memory usage is above 250Mi, the minimum memory recommendation of Tiny and the VPA; below it, both environments
  return the minimum and memory is not really compared;
- their load does not repeat with a period that is a multiple of 1 min.

The names in `w2c-workloads.txt` must match the namespace and name of the Deployments; a Deployment that is not listed
goes to the VPA. To use other workloads, edit the list.

## Dependent variables

The comparison uses the target recommendation, read from the status of the recommendation object of each workload:

- CPU target, in millicores;
- memory target, in MiB;
- absolute error between the targets of both environments;
- relative error between the targets of both environments.

The targets of both environments are collected in the same way and paired by the time elapsed since the start of the
run. Samples taken before a recommender publishes its first recommendation for a workload have no pair and are not
compared. The results are reported separately for the listed workloads (VHAPE `sma` vs Tiny) and for the others (VHAPE
`vpa` vs upstream VPA).

**Absolute error**, computed separately for CPU and memory: for each pair, the absolute difference between the two
targets. Reported per workload and over all workloads: mean absolute error and largest absolute error.

**Relative error**, computed separately for CPU and memory: the absolute error of each pair divided by the target in
the Tiny + VPA environment (the reference), as a percentage. Reported per workload and over all workloads: mean
relative error and largest relative error.

## Requirements

- Two clusters as described in [Design](#design), running Kubernetes 1.28 or later (required by VHAPE), each with the
  metrics-server installed and its default `--metric-resolution` of 60s.
- On the machine that runs the commands: `kubectl`, Helm 3, and this repository cloned on the `paper` branch (all
  commands run from the repository root):

  ```bash
  git clone -b paper https://github.com/ufcg-lsd/vhape-recommender.git
  ```

- A Tiny image, in a registry the Tiny + VPA cluster can pull from: the published image
  `vtexlsd/tiny-serverless-recommender:paper`, or one built with [`../../tiny/build.sh`](../../tiny/README.md#build)
  (then change the image in `tiny-recommender-suite-3.yaml`).
- A VHAPE recommender image with the `sma` and `vpa` heuristics, in a registry the VHAPE cluster can pull from: the
  published image `vtexlsd/vhape-recommender:paper`, or one built from this branch with the
  [release guide](../../../vertical-pod-autoscaler/vhape-docs/release-guide.md).
- The workload manifests (see [Workloads](#workloads)).

## Steps

### In both environments

1. Add the repository of the upstream VPA chart:

   ```bash
   helm repo add autoscalers https://kubernetes.github.io/autoscaler
   ```

   ```bash
   helm repo update
   ```

### Tiny + VPA environment

1. Install the upstream VPA 1.6.0 (chart `0.9.0`): recommender, updater, admission controller and the
   VerticalPodAutoscaler CRDs, one replica each:

   ```bash
   helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler --version 0.9.0 --namespace kube-system --set recommender.enabled=true --set recommender.replicas=1 --set updater.replicas=1 --set admissionController.replicas=1 --set 'updater.extraArgs={--min-replicas=1}' --wait
   ```

2. Create the service account and the roles of Tiny (from the 2021 VPA release Tiny is built on):

   ```bash
   kubectl apply -f reproduction/tiny/deploy/vpa-rbac.yaml
   ```

3. Allow Tiny to write the recommendations into the status of the objects (compat patch 3, see
   [`../../tiny/PATCHES.md`](../../tiny/PATCHES.md)):

   ```bash
   kubectl apply -f reproduction/tiny/deploy/tiny-status-rbac.yaml
   ```

4. Start Tiny, every minute, handling the objects of all namespaces:

   ```bash
   kubectl apply -f reproduction/tiny/deploy/tiny-recommender-suite-3.yaml
   ```

5. Apply the workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

6. Start the script that creates one recommendation object, with `updateMode: Off`, per Deployment of every namespace,
   assigned to Tiny or to the upstream VPA as described in [Design](#design). It runs in the background and also
   handles Deployments created later; it can be stopped once every workload has its recommendation object (the objects
   are kept). Start it at the same point in both environments, once the initial pods of the workloads are running:

   ```bash
   reproduction/experiments/suite-3/create-vpa-objects.sh tiny-vpa '.*' &
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

3. Create both policies:

   ```bash
   kubectl apply -f reproduction/policies/sma-policy.yaml -f reproduction/policies/vpa-policy.yaml
   ```

4. Apply the same workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

5. Start the same script with `vhape`: each object gets `spec.recommenders: [vhape-recommender]` and the annotation
   `vhape/policy: sma-policy` (listed Deployments) or `vhape/policy: vpa-policy` (the others):

   ```bash
   reproduction/experiments/suite-3/create-vpa-objects.sh vhape '.*' &
   ```

## Between runs

Each run uses new clusters, set up from scratch with the steps above, so no state (recommendation history,
checkpoints, objects) carries over from one run to the next.

## Duration and repetitions

To be defined.
