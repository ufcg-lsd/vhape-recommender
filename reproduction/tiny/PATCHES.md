# Compatibility patches applied to Tiny Autoscalers

Tiny Autoscalers was written in 2021 on top of the VPA release `kubernetes/autoscaler@b03f204804c9`
(client-go 0.18, Kubernetes 1.18 era). To run it on a current cluster (the experiments used Kubernetes 1.35.1)
four lines had to change. **None of them touches the recommendation heuristic**: the files written or modified by the
Tiny authors (`util/observation.go`, `logic/estimator.go`, `logic/recommender.go`, `model/aggregate_container_state.go`,
`util/histogram.go`, `main.go`, ...) are unchanged.

| # | File | Where | Applied |
|---|---|---|---|
| 1 | `pkg/recommender/input/controller_fetcher/controller_fetcher.go` | Tiny code (VPA file the authors did not modify) | already in `serverless-recommender/` |
| 2 | `pkg/target/fetcher.go` | VPA base | by `build.sh` (`patches/vpa-base.patch`) |
| 3 | `pkg/utils/vpa/api.go` | VPA base | by `build.sh` (`patches/vpa-base.patch`) |
| 4 | `pkg/recommender/input/cluster_feeder.go` | Tiny code (VPA file the authors did not modify) | already in `serverless-recommender/` |

`patches/tiny-code.patch` documents patches 1 and 4 against the original Tiny code: reverse-applying it to
`serverless-recommender/` gives back `ZhaoNeil/On-Demand-Resizing@f3a15a34` byte for byte.
Each patched line carries the marker `compat patch (harness only)`; `build.sh` fails if any marker is missing.

## 1 and 2 — CronJob informer removed

**Problem.** At startup the recommender builds one informer per controller kind (Deployment, Job, CronJob, ...) and
waits for all of them with `cache.WaitForCacheSync`. The CronJob informer uses `batch/v1beta1`, removed in
Kubernetes 1.25.

**Symptom.** The informer never syncs, so the recommender blocks forever before its main loop. The pod is `Running`
and `Ready` but never logs "Recommender Run" and never writes a recommendation — there is no error.

**Fix.** Remove the `cronJob` entry from the informer map (both files hold the same list; `pkg/target/fetcher.go` is the
one used by `main.go`). Switching to `batch/v1` is not possible without upgrading client-go, which only gained the
`batch/v1` CronJob informer in 0.21.

**Effect.** VerticalPodAutoscaler objects whose target is a CronJob are no longer resolved. Other targets
(Deployments in the experiments) are unaffected.

## 3 — Recommendations written through `/status`

**Problem.** The recommender writes `status.recommendation` with a JSON patch sent to the main resource. Modern VPA CRDs
(needed by VHAPE and by the upstream VPA) enable the `status` subresource, and the API server then silently drops
`.status` changes sent to the main resource (HTTP 200, object unchanged).

**Symptom.** The recommendation stays frozen at the last value written before the CRD changed; `resourceVersion` stops
changing; no error is logged.

**Fix.** Send the same patch to the `status` subresource (`vpaClient.Patch(..., "status")` in `patchVpa`, used only for
status updates). Requires `deploy/tiny-status-rbac.yaml`, since `verticalpodautoscalers/status` is a separate resource
for RBAC.

**Effect.** None on the computation: the same status is published.

## 4 — Objects owned by VHAPE are ignored

**Problem.** The 2021 API has no `spec.recommenders`. The recommender lists every VerticalPodAutoscaler in the namespace
given by `--vpa-object-namespace` and writes its recommendation into all of them, including objects meant for VHAPE.

**Fix.** In `LoadVPAs`, skip objects annotated with `vhape/policy` (only VHAPE objects carry it). The filtered list feeds
both the internal model and `ObservedVpas`, so those objects get no samples, checkpoints or recommendations from Tiny.

**Effect.** Tiny and VHAPE can observe the same pod from the same namespace, so both read exactly the same metrics.
Objects without the annotation are handled exactly as before.

**Known side effect.** Tiny's checkpoint garbage collection (every 10 min, all namespaces) deletes checkpoints of objects
outside its model, which includes those written by VHAPE's legacy pipeline. VHAPE does not use them to recommend.
