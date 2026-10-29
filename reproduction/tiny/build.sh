#!/usr/bin/env bash
# Builds the Tiny Autoscalers serverless-recommender image on top of the VPA release it was written for.
#
#   1. fetches kubernetes/autoscaler at VPA_BASE_COMMIT (only vertical-pod-autoscaler/);
#   2. replaces pkg/recommender with tiny/serverless-recommender (Tiny code, compat patches already applied);
#   3. applies patches/vpa-base.patch to the VPA base;
#   4. builds the binary and the image with Tiny's own Dockerfile.
#
# Usage: tiny/build.sh [image]   (default image: tiny-serverless-recommender:repro)
# WORK=<dir> overrides the build directory (default: tiny/.build).
set -euo pipefail

TINY_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=../versions.env
source "$TINY_DIR/../versions.env"
IMAGE=${1:-tiny-serverless-recommender:repro}
WORK=${WORK:-$TINY_DIR/.build}
SRC=$WORK/autoscaler

if [ ! -d "$SRC/.git" ]; then
  mkdir -p "$SRC"
  git -C "$SRC" init -q
  git -C "$SRC" remote add origin "$VPA_BASE_REPO"
  git -C "$SRC" sparse-checkout init --cone
  git -C "$SRC" sparse-checkout set vertical-pod-autoscaler
  git -C "$SRC" fetch -q --shallow-since="$VPA_BASE_SHALLOW_SINCE" origin master
fi
git -C "$SRC" checkout -q --force "$VPA_BASE_COMMIT"
git -C "$SRC" clean -q -fdx vertical-pod-autoscaler/pkg

VPA=$SRC/vertical-pod-autoscaler
rm -rf "$VPA/pkg/recommender"
cp -r "$TINY_DIR/serverless-recommender" "$VPA/pkg/recommender"
git -C "$SRC" apply --directory=vertical-pod-autoscaler "$TINY_DIR/patches/vpa-base.patch"

# Every compat patch leaves a marker; fail instead of building an unpatched recommender.
for f in pkg/target/fetcher.go pkg/utils/vpa/api.go pkg/recommender/input/controller_fetcher/controller_fetcher.go \
  pkg/recommender/input/cluster_feeder.go; do
  grep -q "compat patch (harness only)" "$VPA/$f" || { echo "compat patch missing in $f" >&2; exit 1; }
done

(cd "$VPA" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -o pkg/recommender/recommender ./pkg/recommender)
docker build -t "$IMAGE" -f "$VPA/pkg/recommender/Dockerfile" "$VPA/pkg/recommender"

echo "built $IMAGE (VPA base $VPA_BASE_COMMIT + Tiny $TINY_COMMIT + compat patches)"
echo "RBAC of the VPA base: $VPA/deploy/vpa-rbac.yaml"
