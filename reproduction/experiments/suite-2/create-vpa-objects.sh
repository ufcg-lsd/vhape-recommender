#!/usr/bin/env bash
# Creates one VerticalPodAutoscaler object per Deployment of the namespaces that match a regular expression, as the VHAPE
# watcher does with a VhapeWatchedNamespaceRegex (same name as the Deployment, updateMode Off, controlledValues
# RequestsOnly for every container, owner reference to the Deployment), for Tiny, which has no watcher (suite 2).
# The objects have no spec.recommenders; Tiny handles every object without the vhape/policy annotation.
# It keeps running, checking every few seconds, and creates the object of every matching Deployment that has none: the
# Deployments created later, in any matching namespace, and the ones whose object was deleted. The objects missing at a
# check are created together, with a single kubectl call; a failed creation is logged and retried on the next check.
# Stop it with Ctrl+C, or with kill %1 when started in the background with &; the objects are kept. The owner reference
# makes Kubernetes delete the object together with its Deployment.
# As with the watcher chart, the namespaces kube-system and monitoring are ignored.
# Usage: create-vpa-objects.sh <namespace-regex>
#   namespace-regex: e.g. '^my-namespace$' for one namespace, '^exp-.*$' for several
set -uo pipefail

REGEX=${1:?usage: create-vpa-objects.sh <namespace-regex>}
CHECK_INTERVAL=2  # seconds
IGNORED_NAMESPACES=" kube-system monitoring "

errors=$(mktemp)
trap 'rm -f "$errors"' EXIT

log() {
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*"
}

# Prints "namespace name [uid]" per object; on failure, logs the kubectl error and returns non-zero.
list() {
  local resource=$1 columns=$2
  if ! kubectl get "$resource" --all-namespaces --no-headers -o custom-columns="$columns" 2>"$errors"; then
    log "error listing $resource, retrying: $(tr '\n' ' ' <"$errors")" >&2
    return 1
  fi
}

# Prints the manifest of the object of a Deployment.
manifest() {
  local namespace=$1 name=$2 uid=$3
  cat <<EOF
---
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: $name
  namespace: $namespace
  ownerReferences:
  - {apiVersion: apps/v1, kind: Deployment, name: $name, uid: $uid, controller: true}
spec:
  targetRef: {apiVersion: apps/v1, kind: Deployment, name: $name}
  updatePolicy: {updateMode: "Off"}
  resourcePolicy:
    containerPolicies:
    - containerName: "*"
      controlledValues: RequestsOnly
EOF
}

log "creating objects with updateMode Off for the Deployments of the namespaces matching '$REGEX'"
while true; do
  if objects=$(list verticalpodautoscalers NAMESPACE:.metadata.namespace,NAME:.metadata.name) &&
    deployments=$(list deployments NAMESPACE:.metadata.namespace,NAME:.metadata.name,UID:.metadata.uid); then
    # An object is found by namespace and name, as the target of the objects created here has the same name.
    existing=" $(awk '{printf "%s/%s ", $1, $2}' <<<"$objects")"
    batch="" sent=""
    while read -r namespace name uid; do
      [[ -n $namespace ]] || continue
      case "$IGNORED_NAMESPACES" in *" $namespace "*) continue ;; esac
      [[ $namespace =~ $REGEX ]] || continue
      case "$existing" in *" $namespace/$name "*) continue ;; esac
      batch+="$(manifest "$namespace" "$name" "$uid")"$'\n'
      sent+="$namespace/$name "
    done <<<"$deployments"
    if [[ -n $batch ]]; then
      # kubectl creates each object on its own, so one failure does not stop the others.
      kubectl apply -f - <<<"$batch" >/dev/null 2>"$errors" ||
        log "error creating objects, retrying: $(tr '\n' ' ' <"$errors")"
      # The objects are listed again to log the ones created; the missing ones are retried on the next check.
      if objects=$(list verticalpodautoscalers NAMESPACE:.metadata.namespace,NAME:.metadata.name); then
        existing=" $(awk '{printf "%s/%s ", $1, $2}' <<<"$objects")"
        for object in $sent; do
          case "$existing" in *" $object "*) log "created $object" ;; esac
        done
      fi
    fi
  fi
  sleep "$CHECK_INTERVAL"
done
