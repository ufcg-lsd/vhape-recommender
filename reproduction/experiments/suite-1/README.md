# Suite 1: upstream VPA vs VHAPE `vpa`

What is common to every suite (environments, controlled variables, workloads, error metrics) is described in
[The experiment suites](../../README.md#the-experiment-suites), and the requirements in
[Running a suite](../../README.md#running-a-suite).

## The experiment

**Goal.** Show that VHAPE, running the `vpa` heuristic, reproduces the upstream VPA 1.6.0 recommender when the
recommendations are applied to the workloads. If it does, the upstream VPA behavior is also available in VHAPE, by
selecting the `vpa` heuristic in a policy.

**Question.** Given the same workloads, does VHAPE with `vpa-policy` allocate the same resources over time as the
upstream VPA recommender?

**Hypothesis.** The requests applied to the pods are the same in both environments.

## Design

**Independent variable:** the recommender.

| | Upstream VPA environment | VHAPE environment |
|---|---|---|
| Recommender | upstream VPA recommender 1.6.0 | VHAPE recommender with `vpa-policy` (`vpa: {}` for CPU and memory, i.e. the VPA defaults) |
| Recommendation objects | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vpa` | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vhape` |

**Controlled variables** specific to this suite:

- **Update mode:** `Recreate` (the updater evicts a pod whose requests are outside the recommended range; the admission
  controller sets the recommended requests on the new pod). The objects are created with `updateMode: Off` before the
  workloads and switched to `Recreate` once the initial pods are running, at the same point in both environments (see
  [Recommendation objects](#recommendation-objects)).
- **Recommender interval:** 1 min, the default of both recommenders.

## Measurements

The compared values are the requests applied to the pods, i.e. the recommendations as they reach the workloads. They
are collected every minute (the recommender interval) from the start of the run; the *n*-th sample of a workload in
the VHAPE environment is paired with the *n*-th sample of the same workload in the upstream VPA environment.

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
   kubectl apply -f reproduction/policies/vpa-policy.yaml
   ```

4. Start the same script with `vhape` and `Off`: each object gets the annotation `vhape/policy: vpa-policy` and
   `spec.recommenders: [vhape-recommender]`. The same script is used in both environments so that their objects come
   from the same code, created at the same pace:

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
