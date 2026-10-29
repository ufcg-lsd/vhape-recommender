# VHAPE with the `sma` and `vpa` heuristics

The code is in this repository, branch `paper` (base: master `89eb18045`, VHAPE 1.3.0):

| Commit | Change |
|---|---|
| `58fe116ff` | `sma` heuristic — reproduces the Tiny Autoscalers `serverless-recommender` |
| `9323dd9d5` | `vpa` heuristic — reproduces the upstream VPA recommender 1.6.0 |
| `ca7e78295` | `extraArgs` value in the `vhape-recommender` chart |

Files: `vertical-pod-autoscaler/pkg/recommender/logic/estimators/{sma,vpa}.go` (+ tests) and the `sma`/`vpa` blocks in
`vertical-pod-autoscaler/charts/vhape-recommender/crds/vhapepolicy-crd.yaml`.

## The heuristics

### `sma` (Tiny Autoscalers)

Policy parameters: `N` (samples per window) and `input` (number of windows). `N: 3, input: 2` is Tiny's
`Predict_SMA(3, 2)`. Fixed as in the original: 15% safety margin, confidence multipliers (upper 1.0/1.0, lower
0.001/−2.0), the integer conversions of the VPA decorator chain and the division by zero when exactly `N` samples exist.
The sample buffer is unbounded, as in the original. Tiny uses SMA only for CPU.

### `vpa` (upstream VPA 1.6.0)

Every parameter is optional and defaults to the VPA flag default, so `vpa: {}` is the stock VPA recommender:

| Parameter | Default | VPA flag |
|---|---|---|
| `targetPercentile` | 0.9 | `--target-{cpu,memory}-percentile` |
| `lowerBoundPercentile` | 0.5 | `--recommendation-lower-bound-{cpu,memory}-percentile` |
| `upperBoundPercentile` | 0.95 | `--recommendation-upper-bound-{cpu,memory}-percentile` |
| `safetyMarginFraction` | 0.15 | `--recommendation-margin-fraction` |
| `confidenceInterval` | 24h | `--confidence-interval-{cpu,memory}` |
| `histogramDecayHalfLife` | 24h | `--{cpu,memory}-histogram-decay-half-life` |
| `memoryAggregationInterval` (memory only) | 24h | `--memory-aggregation-interval` |

In the VPA these are global flags; here they are set per resource and per policy. Fixed as in the VPA: confidence
multipliers, CPU sample weight 0.1 and 5% histogram bucket growth. It reuses the decaying histogram code that VHAPE
keeps from the VPA.

Not reproduced (the heuristic interface does not provide them): OOM events, checkpoints (history lives in memory only),
pod identity (with several replicas, instances are matched by position) and sample timestamps (the arrival time is used,
which makes the bounds differ by up to ~1% from the VPA; the target is not affected).

### Both

The recommender delivers the latest metrics-server reading on every cycle. Both heuristics discard a batch identical to
the last accepted one unless 15s have passed, so they see one sample per real reading, like the VPA. **15s must match
the metrics-server `--metric-resolution`** (the experiments used 15s; with the metrics-server default of 60s, a value
that really repeats, e.g. an idle container at 0, would be accepted up to four times per reading).

## Build the image

Official VHAPE build, from this branch:

```bash
git clone -b paper https://github.com/ufcg-lsd/vhape-recommender.git
cd vhape-recommender/vertical-pod-autoscaler/pkg/recommender
make docker-build-amd64 REGISTRY=<registry> TAG=<tag>   # image <registry>/vhape-recommender-amd64:<tag>
```

`make docker-build` builds every architecture in `ALL_ARCHITECTURES`. The Dockerfile pulls `golang:1.25.7` from Docker
Hub; if Docker reports `401 Unauthorized: incorrect username or password`, the local Docker has stale Docker Hub
credentials — `docker logout` (public images need no login) or `docker login` fixes it.

Fallback without Docker Hub (same binary, same `distroless/static:nonroot` base):

```bash
cd vhape-recommender/vertical-pod-autoscaler
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/vhape/recommender ./pkg/recommender
printf 'FROM gcr.io/distroless/static:nonroot\nCOPY recommender /recommender\nENTRYPOINT ["/recommender"]\n' > /tmp/vhape/Dockerfile
docker build -t <registry>/vhape-recommender-amd64:<tag> /tmp/vhape
```

## Install

With the chart of this branch and its default values; only the image must be set (the default image is the official
one, without `sma` and `vpa`):

```bash
helm install vhape-recommender vertical-pod-autoscaler/charts/vhape-recommender -n kube-system \
  --set image.repository=<registry>/vhape-recommender-amd64 --set image.tag=<tag> \
  --set 'extraArgs={--recommender-interval=1s}'
```

- `--recommender-interval=1s` matches Tiny's polling interval and was used in the comparisons with Tiny. The chart
  default is 1 min; use the same interval on both sides of a comparison.
- The chart requests 4 CPUs and 8Gi for the recommender; lower `resources` on small clusters.
- Helm only installs CRDs on the first `install`. On a cluster that already runs VHAPE, apply the CRD of this branch by
  hand: `kubectl apply -f vertical-pod-autoscaler/charts/vhape-recommender/crds/vhapepolicy-crd.yaml`.
- The VerticalPodAutoscaler CRDs come from `vertical-pod-autoscaler/charts/vertical-pod-autoscaler/crds`.

## Use

Apply a policy from `policies/` and point a VerticalPodAutoscaler object at it. VhapePolicies are immutable: to change a
parameter, create a new policy and update the annotation.

```bash
kubectl apply -f reproduction/vhape/policies/sma-policy.yaml   # or vpa-policy.yaml
```

```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: app-vhape
  namespace: <namespace>
  annotations:
    vhape/policy: sma-policy
spec:
  targetRef: {apiVersion: apps/v1, kind: Deployment, name: app}
  recommenders:
    - name: vhape-recommender
  updatePolicy: {updateMode: "Off"}
```

To compare on the same pod, create one object per recommender pointing at the same Deployment:

- **Tiny**: same namespace, no `spec.recommenders`, no `vhape/policy` (see `../tiny/README.md`);
- **upstream VPA**: no `spec.recommenders` (recommender `default`), in a namespace Tiny does not watch.
