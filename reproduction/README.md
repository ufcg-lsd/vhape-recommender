# Reproducing the VHAPE experiments

This directory holds what is needed to run the experiments of the VHAPE paper again: the VHAPE policies, the Tiny
Autoscalers code rebuilt for current Kubernetes, and the setup and steps of each experiment.

| Path | Contents |
|---|---|
| [`policies/`](policies) | the VhapePolicies used in the experiments |
| [`experiments/`](experiments) | one directory per experiment suite, with its README and scripts |
| [`tiny/`](tiny/README.md) | the Tiny Autoscalers source, the patches that rebuild it for current Kubernetes and its manifests |
| [`versions.env`](versions.env) | the pinned versions of every component |

## The heuristics

VHAPE decides recommendations through *heuristics*, chosen per resource (CPU, memory) in a VhapePolicy. For the paper,
two heuristics were added to VHAPE, each reproducing one of the recommenders it is compared with: `sma` reproduces Tiny
Autoscalers and `vpa` reproduces the upstream VPA recommender 1.6.0. Their code is part of the VHAPE recommender, in
`vertical-pod-autoscaler/pkg/recommender/logic/estimators/` of this branch (`paper`, which starts from master
`89eb18045`, VHAPE 1.3.0).

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

## The experiment suites

Each experiment is organized as a *suite*: a comparison between VHAPE and a baseline, run under the same conditions,
with its own README and scripts. Suites 1 and 2 answer RQ1 of the paper: each compares the recommendations of one
heuristic in VHAPE with those of the recommender it reproduces. Suite 3 answers RQ2: it compares the resource
consumption of a single VHAPE recommender running both heuristics, one per workload, with that of the two original
recommenders running side by side.

| Suite | RQ | Baseline | VHAPE policy | Workloads | Compared | Recommender interval | Update mode |
|---|---|---|---|---|---|---|---|
| [suite 1](experiments/suite-1/README.md) | RQ1 | upstream VPA 1.6.0 | [`vpa-policy`](policies/vpa-policy.yaml) | all | CPU and memory targets | 1 min | `Recreate` |
| [suite 2](experiments/suite-2/README.md) | RQ1 | Tiny Autoscalers | [`sma-policy`](policies/sma-policy.yaml) | short-duration | CPU targets | 1s | `Off` |
| [suite 3](experiments/suite-3/README.md) | RQ2 | Tiny Autoscalers and upstream VPA 1.6.0, side by side | `sma-policy` for short-duration workloads, `vpa-policy` for the others | all | CPU and memory consumption of the recommenders | 1 min | `Off` |

### Environments

Each suite has two environments, the baseline and VHAPE, in two separate clusters set up in the same way.
The VHAPE cluster runs a single VHAPE recommender; the baseline cluster runs the baseline recommender (in suite 3, Tiny and the upstream VPA side by side).

Both clusters must run the same workloads with the same usage over time, so that both recommenders receive the same
samples. In the paper, both clusters were emulated, replaying the same trace; any setup that gives both clusters the
same usage can be used instead.

### Running a suite

Requirements:

- Two clusters, one per environment, running Kubernetes 1.28 or later (required by VHAPE), each with the
  metrics-server installed.
- On the machine that runs the commands: `kubectl`, Helm 3, and this repository cloned on the `paper` branch (all
  commands run from the repository root):

  ```bash
  git clone -b paper https://github.com/ufcg-lsd/vhape-recommender.git
  ```

- The images of the recommenders, in a registry the clusters can pull from. The experiments use published images, so
  nothing needs to be built:

  | Recommender | Image | To rebuild it |
  |---|---|---|
  | VHAPE | `vtexlsd/vhape-recommender:paper` | [release guide](../vertical-pod-autoscaler/vhape-docs/release-guide.md), from this branch |
  | Tiny Autoscalers | `vtexlsd/tiny-serverless-recommender:paper` (rebuilt for current Kubernetes) | [`tiny/`](tiny/README.md) |
  | upstream VPA 1.6.0 | `registry.k8s.io/autoscaling/vpa-*:1.6.0` (official, unchanged) | — |

Then follow the steps in the README of the desired suite.
