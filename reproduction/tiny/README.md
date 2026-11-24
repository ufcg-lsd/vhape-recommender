# Tiny Autoscalers (baseline for the `sma` heuristic)

Tiny Autoscalers (Zhao and Uta, *Tiny Autoscalers for Tiny Workloads: Dynamic CPU Allocation for Serverless
Functions*, CCGrid 2022) replaces the recommender of a 2021 VPA release with moving-average predictors. Its
`serverless-recommender` is the baseline of the `sma` heuristic of VHAPE. This directory holds its code, with the
patches needed to run it on current Kubernetes.

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

The image is published as `vtexlsd/tiny-serverless-recommender:paper`, and the experiments use it. Building another one requires Go (the experiments used 1.27.3) and Docker:

```bash
reproduction/tiny/build.sh                          # image tiny-serverless-recommender:repro
reproduction/tiny/build.sh <registry>/tiny-sma:tag  # custom name
```

## Deploy

Tiny needs the VerticalPodAutoscaler CRDs (the suites install them with the upstream VPA chart), a service account
with the VPA recommender roles, and write access to the status of the objects:

```bash
kubectl apply -f reproduction/tiny/deploy/vpa-rbac.yaml          # service account and roles
kubectl apply -f reproduction/tiny/deploy/tiny-status-rbac.yaml  # write access to verticalpodautoscalers/status
```

Then start the recommender with one of the manifests:

| Manifest | Interval | Used in |
|---|---|---|
| [`tiny-recommender-paper.yaml`](deploy/tiny-recommender-paper.yaml) | 1s (Tiny's default) | [suite 2](../experiments/suite-2/README.md) |
| [`tiny-recommender-suite-3.yaml`](deploy/tiny-recommender-suite-3.yaml) | 1 min | [suite 3](../experiments/suite-3/README.md) |

Both use the published image; to use another one, change `image` in the manifest.

### How it selects objects

The 2021 VPA API that Tiny is built on has no `spec.recommenders`, so Tiny ignores that field. It handles every
VerticalPodAutoscaler object that does **not** carry the `vhape/policy` annotation (compat patch 4), in all
namespaces.

When Tiny runs next to the upstream VPA recommender, as in suite 3, each object must be marked for one of them: an
object for Tiny lists only another recommender name in `spec.recommenders` (e.g. `tiny`), which the upstream VPA skips;
an object for the upstream VPA carries the `vhape/policy` annotation, which Tiny skips.
