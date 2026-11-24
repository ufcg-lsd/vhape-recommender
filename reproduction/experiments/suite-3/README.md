# Suite 3: Tiny and upstream VPA together vs VHAPE with both heuristics

What is common to every suite (environments, controlled variables, workloads, error metrics) is described in
[The experiment suites](../../README.md#the-experiment-suites), and the requirements in
[Running a suite](../../README.md#running-a-suite).

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

**Independent variable:** the recommenders. Each workload is assigned to a heuristic by its name: the Deployments
listed in [`w2c-workloads.txt`](w2c-workloads.txt) (one `namespace/name` per line) go to the SMA, every other
Deployment goes to the VPA. The names in the list must match the namespace and name of the Deployments; to use other
workloads, edit the list.

| | Tiny + VPA environment | VHAPE environment |
|---|---|---|
| Recommenders | Tiny `serverless-recommender` for the listed Deployments, upstream VPA recommender 1.6.0 for the others | one VHAPE recommender: `sma-policy` for the listed Deployments, `vpa-policy` for the others |
| Recommendation objects | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `tiny-vpa` | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vhape` |

In the Tiny + VPA environment, each object is marked so that only one of the two recommenders handles it:

- **listed Deployments:** `spec.recommenders: [tiny]`. The upstream VPA skips objects that do not list its name; Tiny
  ignores the field (the 2021 VPA API it is built on does not have it).
- **other Deployments:** the annotation `vhape/policy: vpa-policy`. Tiny skips objects with this annotation (compat
  patch, see [`../../tiny/PATCHES.md`](../../tiny/PATCHES.md)); the upstream VPA ignores it.

**Controlled variables** specific to this suite:

- **Update mode:** `Off`, as in suite 2, so both environments run the same pods.
- **Recommender interval:** 1 min for every recommender. This is the default of the upstream VPA and of VHAPE; Tiny is
  set to 1 min in its manifest ([`tiny-recommender-suite-3.yaml`](../../tiny/deploy/tiny-recommender-suite-3.yaml)),
  instead of its default of 1s, so that all recommenders run at the same interval.

## Measurements

The compared values are the target recommendations, read from the status of the recommendation object of each
workload, paired by the time elapsed since the start of the run. Samples taken before a recommender publishes its
first recommendation for a workload have no pair and are not compared. The results are reported separately for the
listed workloads (VHAPE `sma` vs Tiny) and for the others (VHAPE `vpa` vs upstream VPA).

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
