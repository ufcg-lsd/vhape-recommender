# Experiments

Each suite compares one recommender with its VHAPE counterpart:

| Suite | Baseline | VHAPE policy | Recommender interval | Update mode |
|---|---|---|---|---|
| [`suite-1/`](suite-1/README.md) | upstream VPA 1.6.0 | `vpa-policy` (`vpa` for CPU and memory) | 1 min (default of both) | `Recreate` (recommendations applied) |
| [`suite-2/`](suite-2/README.md) | Tiny Autoscalers | `sma-policy` (`sma` for CPU, `vpa` for memory) | 1s (Tiny's default) | `Off` (recommendations only) |

In every suite, the baseline and VHAPE run in two separate and identical clusters, with the same workloads, started at
the same time; each cluster runs a single recommender. The README of each suite describes the experiment and gives the
steps to run it from scratch.

In the VHAPE environments, the VHAPE watcher creates the recommendation objects. In the baseline environments, which
have no watcher, the `create-vpa-objects.sh` script of each suite ([`suite-1/`](suite-1/create-vpa-objects.sh) with
`Recreate`, [`suite-2/`](suite-2/create-vpa-objects.sh) with `Off`) creates the same objects for the Deployments of the
namespaces that match a regular expression.
