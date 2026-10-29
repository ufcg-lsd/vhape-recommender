# Reproduction material

Material to rebuild the recommenders compared in the paper:

| Directory | What | Compared with |
|---|---|---|
| [`vhape/`](vhape/README.md) | VHAPE with the `sma` and `vpa` heuristics (code in this branch) | — |
| [`tiny/`](tiny/README.md) | Tiny Autoscalers `serverless-recommender`, rebuilt for current Kubernetes | VHAPE `sma` |
| upstream VPA 1.6.0 | official images `registry.k8s.io/autoscaling/vpa-*:1.6.0`, no changes | VHAPE `vpa` |

All versions are pinned in [`versions.env`](versions.env).

## Order

1. Build the images: `vhape/README.md` (official VHAPE build) and `tiny/build.sh`.
2. Make the images available to the cluster nodes (registry, or `minikube image load`/`kind load`).
3. Install the VerticalPodAutoscaler CRDs, VHAPE (Helm chart of this branch) and, if needed, Tiny (`tiny/deploy/`)
   and/or the upstream VPA recommender.
4. Set the metrics-server `--metric-resolution` to 15s (see `vhape/README.md`, section "Both").
5. Apply a policy from `vhape/policies/` and create the VerticalPodAutoscaler objects.

## What the experiments showed

- `sma` vs Tiny, same pod, `updateMode: Off`: CPU target identical in every sample; bounds within ~1% (sample timestamps).
- `vpa` vs upstream VPA 1.6.0, same pod, `updateMode: Off`: CPU and memory target identical in every sample; bounds within
  ~1.3%.
- `sma` vs Tiny with recommendations applied (`updateMode: Recreate`, VPA 1.6 updater and admission controller): same
  evictions at the same time with the same requests.
