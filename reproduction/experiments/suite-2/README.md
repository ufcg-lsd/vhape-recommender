# Suite 2: Tiny Autoscalers vs VHAPE `sma`

The environments and the workloads common to every suite are described in
[The experiment suites](../../README.md#the-experiment-suites), and the requirements in
[Running a suite](../../README.md#running-a-suite).

## The experiment

Suite 2 answers RQ1 for the `sma` heuristic.

**Goal.** Show that VHAPE, running `sma-policy`, preserves the CPU recommendations of the Tiny Autoscalers SMA
recommender. The recommendations are not applied (`updateMode: Off`).

**Question.** Given the same workloads, does VHAPE with `sma-policy` produce the same CPU target recommendations over
time as Tiny?

**Hypothesis.** The CPU targets are the same in both environments.

**Workloads:** the short-duration workloads.

## Design

**Independent variable:** the recommender.

| | Tiny environment | VHAPE environment |
|---|---|---|
| Recommender | Tiny `serverless-recommender` | VHAPE recommender with `sma-policy` (`sma` with `N: 3, input: 2` for CPU, `vpa: {}` for memory) |
| VerticalPodAutoscaler objects | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `tiny` | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vhape` |

**Controlled variables** specific to this suite:

- **Update mode:** `Off`. The updater and the admission controller do nothing with `Off` objects, so the requests of
  the pods stay as in the workload manifests and both environments run the same pods.
- **VerticalPodAutoscaler objects:** created before the workloads, so both recommenders receive samples from the first
  instant of every pod (VHAPE only feeds the heuristics of existing objects).
- **Recommender interval:** 1s, the default of Tiny (VHAPE is set to 1s through the chart value `extraArgs`). Both
  recommenders poll every second but get a new reading once a minute, so the SMA window (`N: 3, input: 2`, six
  readings) covers about six minutes.

## Measurements

The compared values are the CPU targets (in millicores), read from the status of the VerticalPodAutoscaler objects.
Memory is not compared. The targets of both environments are matched by workload, container and emulation
timestamp. For each pair, the absolute and relative errors are computed as described in the paper.

## Steps

### In both environments

1. Add the repository of the upstream VPA chart:

   ```bash
   helm repo add autoscalers https://kubernetes.github.io/autoscaler
   ```

   ```bash
   helm repo update
   ```

### Tiny environment

1. Install the updater, the admission controller and the VerticalPodAutoscaler CRDs of the upstream VPA 1.6.0,
   one replica each, with the upstream recommender disabled (Tiny takes its place):

   ```bash
   helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler --version 0.9.0 --namespace kube-system --set recommender.enabled=false --set updater.replicas=1 --set admissionController.replicas=1 --set 'updater.extraArgs={--min-replicas=1}' --wait
   ```

2. Create the service account and the roles of the recommender (from the 2021 VPA release Tiny is built on):

   ```bash
   kubectl apply -f reproduction/tiny/deploy/vpa-rbac.yaml
   ```

3. Allow it to write the recommendations into the status of the objects (compat patch 3, see
   [`../../tiny/PATCHES.md`](../../tiny/PATCHES.md)):

   ```bash
   kubectl apply -f reproduction/tiny/deploy/tiny-status-rbac.yaml
   ```

4. Start Tiny:

   ```bash
   kubectl apply -f reproduction/tiny/deploy/tiny-recommender-paper.yaml
   ```

5. Start the script. It runs in the background and also handles Deployments created later; it can be stopped once
   every workload has its object (the objects are kept):

   ```bash
   reproduction/experiments/suite-2/create-vpa-objects.sh tiny '.*' &
   ```

6. Apply the workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

### VHAPE environment

1. Install the updater, the admission controller and the VerticalPodAutoscaler CRDs of the upstream VPA 1.6.0,
   one replica each, with the upstream recommender disabled:

   ```bash
   helm upgrade --install vertical-pod-autoscaler autoscalers/vertical-pod-autoscaler --version 0.9.0 --namespace kube-system --set recommender.enabled=false --set updater.replicas=1 --set admissionController.replicas=1 --set 'updater.extraArgs={--min-replicas=1}' --wait
   ```

2. Install the VHAPE recommender, with the image that has the `sma` and `vpa` heuristics (one replica, every second,
   like Tiny):

   ```bash
   helm upgrade --install vhape-recommender vertical-pod-autoscaler/charts/vhape-recommender --namespace kube-system --set image.tag=paper --set 'extraArgs={--recommender-interval=1s}' --wait
   ```

3. Create the policy with the `sma` heuristic for CPU and the `vpa` heuristic for memory:

   ```bash
   kubectl apply -f reproduction/policies/sma-policy.yaml
   ```

4. Start the script with `vhape`. Each object gets the annotation `vhape/policy: sma-policy` and
   `spec.recommenders: [vhape-recommender]`:

   ```bash
   reproduction/experiments/suite-2/create-vpa-objects.sh vhape '.*' &
   ```

5. Apply the same workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```
