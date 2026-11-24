# Compatibility patches applied to Tiny Autoscalers

Tiny Autoscalers was written in 2021 on top of the VPA release `kubernetes/autoscaler@b03f204804c9` (Kubernetes 1.18
era). To run it on a current cluster (the experiments used Kubernetes 1.35.1), four lines had to change. None of them
touches the recommendation heuristic.

| # | File | Applied |
|---|---|---|
| 1 | `pkg/recommender/input/controller_fetcher/controller_fetcher.go` | already in `serverless-recommender/` |
| 2 | `pkg/target/fetcher.go` | by `build.sh` (`patches/vpa-base.patch`) |
| 3 | `pkg/utils/vpa/api.go` | by `build.sh` (`patches/vpa-base.patch`) |
| 4 | `pkg/recommender/input/cluster_feeder.go` | already in `serverless-recommender/` |

Reverse-applying `patches/tiny-code.patch` to `serverless-recommender/` gives back the original Tiny code
(`ZhaoNeil/On-Demand-Resizing@f3a15a34`) byte for byte.

**1 and 2: CronJob informer removed.** The recommender waits at startup for an informer per controller kind, and the
CronJob one uses `batch/v1beta1`, removed in Kubernetes 1.25. Without the patch the recommender never starts.

**3: Recommendations written to the `status` subresource.** Current VPA CRDs only accept changes to `status` through
the `/status` endpoint of the object, and silently ignore them elsewhere. Tiny writes its recommendation to the object
itself, so it was lost; the patch writes it to `/status`, which requires
[`deploy/tiny-status-rbac.yaml`](deploy/tiny-status-rbac.yaml).

**4: Objects with the `vhape/policy` annotation are skipped.** The 2021 VPA API has no `spec.recommenders`, so Tiny
cannot tell which objects are meant for another recommender. The patch makes it skip objects annotated with
`vhape/policy`; suite 3 uses this to separate the objects of Tiny from those of the upstream VPA.
