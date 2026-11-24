# Suite 2: Tiny Autoscalers vs VHAPE `sma`

What is common to every suite (environments, controlled variables, workloads, error metrics) is described in
[The experiment suites](../../README.md#the-experiment-suites), and the requirements in
[Running a suite](../../README.md#running-a-suite).

## The experiment

**Goal.** Show that VHAPE, running `sma-policy` (`sma` for CPU, `vpa` for memory, as Tiny does), reproduces the
recommendations of the Tiny recommender. If it does, the Tiny behavior is also available in VHAPE, by selecting these
heuristics in a policy. The recommendations are not applied (`updateMode: Off`), as in the evaluation of Tiny in its
repository.

**Question.** Given the same workloads, does VHAPE with `sma-policy` recommend the same resources over time as the Tiny
recommender?

**Hypothesis.** The target recommendations are the same in both environments.

## Design

**Independent variable:** the recommender.

| | Tiny environment | VHAPE environment |
|---|---|---|
| Recommender | Tiny `serverless-recommender` | VHAPE recommender with `sma-policy` (`sma` with `N: 3, input: 2` for CPU, `vpa: {}` for memory) |
| Recommendation objects | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `tiny` | created by [`create-vpa-objects.sh`](create-vpa-objects.sh) `vhape` |

**Controlled variables** specific to this suite:

- **Update mode:** `Off`. The updater and the admission controller do nothing with `Off` objects, so the requests of
  the pods stay as in the workload manifests and both environments run the same pods.
- **Recommender interval:** 1s, the default of Tiny (VHAPE is set to 1s through the chart value `extraArgs`). Both
  recommenders poll every second but get a new reading once a minute, so the SMA window (`N: 3, input: 2`, six
  readings) covers about six minutes.

## Measurements

The compared values are the target recommendations, read from the status of the recommendation object of each
workload, paired by the time elapsed since the start of the run. Before a workload has usage samples, neither
recommender publishes a recommendation for it; those first instants are not compared.

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

4. Start Tiny, with the published image and without `--vpa-object-namespace`, so it handles the recommendation
   objects of all namespaces (it is the only recommender in this cluster):

   ```bash
   kubectl apply -f reproduction/tiny/deploy/tiny-recommender-paper.yaml
   ```

5. Apply the workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

6. Start the script that creates one recommendation object, with `updateMode: Off` and no `spec.recommenders`, per
   Deployment of every namespace. It runs in the background and also handles Deployments created later; it can be
   stopped once every workload has its recommendation object (the objects are kept). Start it at the same point in
   both environments, once the initial pods of the workloads are running, as in suite 1:

   ```bash
   reproduction/experiments/suite-2/create-vpa-objects.sh tiny '.*' &
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

4. Apply the same workloads:

   ```bash
   kubectl apply -f <workload manifests>
   ```

5. Start the same script with `vhape`: each object gets the annotation `vhape/policy: sma-policy` and
   `spec.recommenders: [vhape-recommender]`. The same script is used in both environments so that their objects come
   from the same code, created at the same pace:

   ```bash
   reproduction/experiments/suite-2/create-vpa-objects.sh vhape '.*' &
   ```
