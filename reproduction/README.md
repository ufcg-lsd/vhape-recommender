# Reproduction material

The paper compares VHAPE with two existing recommenders. VHAPE decides recommendations through *heuristics*, chosen
per resource (CPU, memory) in a VhapePolicy. For the paper, two heuristics were added to VHAPE, each reproducing one of
those recommenders. Suites 1 and 2 compare each heuristic with the recommender it reproduces; suite 3 compares a
single VHAPE recommender running both heuristics, one per workload, with the two original recommenders running
together:

| Suite | Baseline | VHAPE policy | Recommender interval | Update mode |
|---|---|---|---|---|
| [suite 1](experiments/suite-1/README.md) | upstream VPA 1.6.0 | [`vpa-policy`](policies/vpa-policy.yaml) (`vpa` for CPU and memory) | 1 min (default of both) | `Recreate` (recommendations applied) |
| [suite 2](experiments/suite-2/README.md) | Tiny Autoscalers | [`sma-policy`](policies/sma-policy.yaml) (`sma` for CPU, `vpa` for memory) | 1s (Tiny's default) | `Off` (recommendations only) |
| [suite 3](experiments/suite-3/README.md) | Tiny Autoscalers and upstream VPA 1.6.0, in the same cluster | `sma-policy` for the Deployments listed in [`w2c-workloads.txt`](experiments/suite-3/w2c-workloads.txt), `vpa-policy` for the others | 1 min (Tiny set to it) | `Off` (recommendations only) |

## Running the experiments

The README of each suite describes the experiment and gives the steps to run it from scratch.

Each suite has two environments, the baseline and VHAPE, in two separate clusters set up in the same way. The VHAPE cluster runs a single VHAPE recommender; the baseline cluster runs the baseline recommender
(in suite 3, Tiny and the upstream VPA side by side). Both clusters must run the same workloads with the same usage over
time, so that both recommenders receive the same samples. In the paper, both clusters were emulated, replaying the
same trace of workloads and usage; any setup that gives both clusters the same usage can be used instead.

In both environments of a suite, the `create-vpa-objects.sh` script of the suite ([`suite-1/`](experiments/suite-1/create-vpa-objects.sh),
[`suite-2/`](experiments/suite-2/create-vpa-objects.sh), [`suite-3/`](experiments/suite-3/create-vpa-objects.sh))
creates one recommendation object per Deployment. Its first argument picks the environment: the baseline (`vpa`,
`tiny` or `tiny-vpa`) or `vhape`.

The experiments use published images, so nothing needs to be built:

| Recommender | Image | To rebuild it |
|---|---|---|
| VHAPE | `vtexlsd/vhape-recommender:paper` | [release guide](../vertical-pod-autoscaler/vhape-docs/release-guide.md), from this branch |
| Tiny Autoscalers | `vtexlsd/tiny-serverless-recommender:paper` (rebuilt for current Kubernetes) | [`tiny/`](tiny/README.md) |
| upstream VPA 1.6.0 | `registry.k8s.io/autoscaling/vpa-*:1.6.0` (official, unchanged) | — |

The VHAPE image is built from this repository, branch `paper`, which starts from master `89eb18045` (VHAPE 1.3.0).

## Contents

| Path | Contents |
|---|---|
| [`policies/`](policies) | the VhapePolicies used in the experiments |
| [`experiments/`](experiments) | one directory per suite, with its README and scripts |
| [`tiny/`](tiny/README.md) | the Tiny Autoscalers source, the patches that rebuild it for current Kubernetes and its manifests |
| [`versions.env`](versions.env) | the pinned versions of every component |

## The heuristics

### `sma` (Tiny Autoscalers)

Tiny Autoscalers recommends CPU with a simple moving average, `Predict_SMA(N, input)`. The `sma` heuristic takes the
same two parameters. The paper uses Tiny's values, `N: 3, input: 2`:

```yaml
cpu:
  scalingHeuristic:
    sma:
      "N": 3
      input: 2
```

For memory, Tiny does not use the moving average: its recommender keeps the VPA percentile estimator. To match it,
[`sma-policy`](policies/sma-policy.yaml) uses the `vpa` heuristic for memory.

### `vpa` (upstream VPA)

The `vpa` heuristic computes recommendations like the upstream VPA recommender: it keeps a histogram of the usage and
recommends percentiles of it, plus a safety margin. Its parameters are the ones the VPA exposes as command-line flags
(the percentiles, the margin, the histogram decay...). All of them are optional, and each one defaults to the value of
the corresponding VPA flag. An empty block therefore behaves as the stock VPA recommender, and this is what the paper
uses ([`vpa-policy`](policies/vpa-policy.yaml)):

```yaml
cpu:
  scalingHeuristic:
    vpa: {}
```

The parameters are listed in the [VhapePolicy CRD](../vertical-pod-autoscaler/charts/vhape-recommender/crds/vhapepolicy-crd.yaml).

Two features of the VPA are not reproduced: OOM events are not taken into account, and the history is kept in memory
only (the VPA also saves it in checkpoints, which survive a restart of the recommender).
