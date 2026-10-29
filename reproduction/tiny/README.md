# Tiny Autoscalers (baseline for the `sma` heuristic)

Tiny Autoscalers (Zhao and Uta, *Tiny Autoscalers for Tiny Workloads: Dynamic CPU Allocation for Serverless
Functions*, CCGrid 2022) replaces the recommender of a 2021 VPA release with SMA/EMA predictors. The `sma` heuristic of
VHAPE reproduces its `serverless-recommender`: `Predict_SMA(3, 2)` for CPU, the VPA safety margin and the confidence
multiplier on the bounds.

## Contents

| Path | Description |
|---|---|
| `serverless-recommender/` | Tiny code from `ZhaoNeil/On-Demand-Resizing@f3a15a34`, with compat patches 1 and 4 applied |
| `patches/tiny-code.patch` | what changed in the Tiny code (reverse-apply it to get the original back) |
| `patches/vpa-base.patch` | compat patches 2 and 3, applied to the VPA base by `build.sh` |
| `PATCHES.md` | why each patch exists; none changes the heuristic |
| `build.sh` | assembles and builds the image |
| `deploy/` | manifests to run it (Tiny has no Helm chart) |

The original repository has no license file. Files derived from the VPA keep their Apache 2.0 headers; the Tiny code is
included unmodified apart from the documented patches, for reproducibility, with credit to its authors.

## Build

Requirements: git, Go (experiments used 1.27.3) and Docker.

```bash
reproduction/tiny/build.sh                          # image tiny-serverless-recommender:repro
reproduction/tiny/build.sh <registry>/tiny-sma:tag  # custom name
```

The script fetches `kubernetes/autoscaler` at `b03f204804c9` (only `vertical-pod-autoscaler/`, cached in `tiny/.build`),
puts `serverless-recommender/` in place of `pkg/recommender`, applies `patches/vpa-base.patch`, checks that all four
patches are present, builds the binary with the vendored dependencies and builds the image with Tiny's own Dockerfile.

## Deploy

Requirements: the VerticalPodAutoscaler CRDs installed (e.g. from VHAPE's `charts/vertical-pod-autoscaler/crds`) and the
image available to the cluster nodes.

```bash
kubectl apply -f reproduction/tiny/deploy/vpa-rbac.yaml          # service account and roles
kubectl apply -f reproduction/tiny/deploy/tiny-status-rbac.yaml  # write access to verticalpodautoscalers/status
# edit --vpa-object-namespace (and the image, if renamed) in tiny-recommender.yaml, then:
kubectl apply -f reproduction/tiny/deploy/tiny-recommender.yaml
```

### How it selects objects

Tiny handles every VerticalPodAutoscaler object in the namespace given by `--vpa-object-namespace` that does **not**
carry the `vhape/policy` annotation. An object for Tiny therefore needs no `spec.recommenders` field:

```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: app-tiny
  namespace: <the namespace in --vpa-object-namespace>
spec:
  targetRef: {apiVersion: apps/v1, kind: Deployment, name: app}
  updatePolicy: {updateMode: "Off"}
```

A VHAPE object for the same Deployment can live in the same namespace (see `../vhape/README.md`); Tiny ignores it.

### Things to know

- Tiny persists its history in VerticalPodAutoscalerCheckpoint objects. To start from scratch, scale the recommender to
  zero **before** deleting the checkpoints, otherwise the running pod writes them again.
- The upstream VPA recommender (recommender name `default`) also handles objects without `spec.recommenders`. Do not
  run it on the namespace used by Tiny.
- Known reproduction of upstream behavior: when exactly N samples exist, `Predict_SMA` divides by zero and the
  recommendation drops to the minimum for one cycle. VHAPE's `sma` heuristic keeps the same behavior on purpose.
