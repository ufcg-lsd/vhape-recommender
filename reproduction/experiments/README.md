# Experiments

Each suite compares one recommender with its VHAPE counterpart, with the recommendations applied to the workloads:

| Suite | Baseline | VHAPE policy | Recommender interval |
|---|---|---|---|
| [`suite-1/`](suite-1/README.md) | upstream VPA 1.6.0 | `vpa-policy` (`vpa` for CPU and memory) | 1 min (default of both) |
| [`suite-2/`](suite-2/README.md) | Tiny Autoscalers | `sma-policy` (`sma` for CPU, `vpa` for memory) | 1s (Tiny's default) |

In every suite, the baseline and VHAPE run in two separate and identical clusters, with the same workloads, started at
the same time; each cluster runs a single recommender. The README of each suite describes the experiment and gives the
steps to run it from scratch.

In the VHAPE environments, the VHAPE watcher creates the recommendation objects. In the baseline environments, which
have no watcher, [`create-vpa-objects.sh`](create-vpa-objects.sh) creates the same objects for the Deployments of the
namespaces that match a regular expression.
