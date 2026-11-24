# Suite 1: upstream VPA vs VHAPE `vpa`

The environments and the workloads common to every suite are described in
[The experiment suites](../../README.md#the-experiment-suites), and the requirements in
[Running a suite](../../README.md#running-a-suite).

## The experiment

Suite 1 answers RQ1 for the `vpa` heuristic.

**Goal.** Show that VHAPE, running the `vpa` heuristic, preserves the recommendations of the upstream VPA 1.6.0
recommender, with the recommendations applied to the workloads.

**Question.** Given the same workloads, does VHAPE with `vpa-policy` produce the same CPU and memory target
recommendations over time as the upstream VPA recommender?

**Hypothesis.** The CPU and memory targets are the same in both environments.

**Workloads:** all the workloads.

## Design

**Independent variable:** the recommender.

| | Upstream VPA environment | VHAPE environment |
|---|---|---|
| Recommender | upstream VPA recommender 1.6.0 | VHAPE recommender with `vpa-policy` (`vpa: {}` for CPU and memory, i.e. the VPA defaults) |
| VerticalPodAutoscaler objects | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vpa` | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vhape` |

**Controlled variables** specific to this suite:

- **Update mode:** `Recreate`: the updater evicts a pod whose requests are outside the recommended range, and the
  admission controller sets the recommended requests on the new pod. See
  [VerticalPodAutoscaler objects](#verticalpodautoscaler-objects) for when the objects are created and switched to
  `Recreate`.
- **Recommender interval:** 1 min, the default of both recommenders.

## Measurements

The compared values are the CPU (in millicores) and memory (in MiB) targets, read from the status of the
VerticalPodAutoscaler objects. The targets of both environments are matched by workload, container, resource and
emulation timestamp. For each pair, the absolute and relative errors are computed as described in the paper.

## Steps

### VerticalPodAutoscaler objects

In both environments, the objects are:

- **created before the workloads**, so both recommenders receive samples from the first instant of every pod. The
  upstream recommender keeps the samples of every pod, with or without an object (`--memory-saver=false`, its
  default); VHAPE feeds only the estimators of existing objects;
- **created with `updateMode: Off`**, so the updater does not evict pods while they are still being placed;
- **switched to `Recreate` once the initial pods are running**, at the same point in both environments. Changing the
  update mode does not reset the recommendation history of either recommender.

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

2. Start the script with `Off`. It runs in the background and also handles Deployments created later:

   ```bash
   reproduction/experiments/suite-1/create-vpa-objects.sh vpa '.*' Off &
   ```

3. Apply the workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

4. Once the initial pods are running, stop the script, switch every object to `Recreate`, and start the script again
   with `Recreate`, so Deployments created later get `Recreate` objects. It can be stopped once every workload has its
   object (the objects are kept):

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

4. Start the script with `vhape` and `Off`. Each object gets the annotation `vhape/policy: vpa-policy` and
   `spec.recommenders: [vhape-recommender]`:

   ```bash
   reproduction/experiments/suite-1/create-vpa-objects.sh vhape '.*' Off &
   ```

5. Apply the same workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

6. At the same point as in the upstream VPA environment, switch to `Recreate` as there:

   ```bash
   kill %1
   ```

   ```bash
   kubectl get vpa -A --no-headers | while read ns name _; do kubectl patch vpa "$name" -n "$ns" --type=merge -p '{"spec":{"updatePolicy":{"updateMode":"Recreate"}}}'; done
   ```

   ```bash
   reproduction/experiments/suite-1/create-vpa-objects.sh vhape '.*' &
   ```
