#!/usr/bin/env bash
# Creates one VerticalPodAutoscaler object per Deployment of the namespaces that match a regular expression (same name as
# the Deployment, updateMode Off, controlledValues RequestsOnly for every container, owner reference to the Deployment),
# and assigns each Deployment to a recommender by its name (suite 3). The Deployments listed in the file given as the
# third argument go to the SMA heuristic; every other Deployment goes to the VPA heuristic.
#   The list has one namespace/name per line: the Deployments of the trace that use the SMA heuristic.
#   tiny-vpa environment (Tiny and the upstream VPA recommender in the same cluster):
#     listed:   spec.recommenders [tiny]; the upstream VPA skips objects that do not list its name, Tiny ignores the field.
#     the rest: annotation vhape/policy; Tiny skips objects with this annotation, the upstream VPA ignores it.
#   vhape environment (one VHAPE recommender), the same object the VHAPE watcher creates:
#     listed:   annotation vhape/policy: sma-policy and spec.recommenders [vhape-recommender].
#     the rest: annotation vhape/policy: vpa-policy and spec.recommenders [vhape-recommender].
#   The VHAPE watcher is not used: it assigns a policy per namespace, and the namespaces of the trace mix listed and
#   unlisted Deployments.
# It keeps running, checking every few seconds, and creates the object of every matching Deployment that has none: the
# Deployments created later, in any matching namespace, and the ones whose object was deleted. The objects missing at a
# check are created together, with a single kubectl call; a failed creation is logged and retried on the next check.
# Stop it with Ctrl+C, or with kill %1 when started in the background with &; the objects are kept. The owner reference
# makes Kubernetes delete the object together with its Deployment.
# As with the watcher chart, the namespaces kube-system and monitoring are ignored.
# Usage: create-vpa-objects.sh <tiny-vpa|vhape> <namespace-regex> <sma-list>
#   namespace-regex: e.g. '^my-namespace$' for one namespace, '^exp-.*$' for several
#   sma-list: file with the Deployments that go to the SMA heuristic, one namespace/name per line
set -uo pipefail

USAGE="usage: create-vpa-objects.sh <tiny-vpa|vhape> <namespace-regex> <sma-list>"
ENVIRONMENT=${1:?$USAGE}
REGEX=${2:?$USAGE}
LIST=${3:?$USAGE}  # one namespace/name per line
[[ -r $LIST ]] || { echo "cannot read the list $LIST" >&2; exit 1; }
CHECK_INTERVAL=2  # seconds
IGNORED_NAMESPACES=" kube-system monitoring "

case "$ENVIRONMENT" in tiny-vpa | vhape) ;; *) echo "$USAGE" >&2; exit 1 ;; esac
listed=" $(tr '\n' ' ' <"$LIST") " || exit 1

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

# Prints the manifest of the object of a Deployment; heuristic is sma or vpa. The metadata and spec lines assign the
# object to its recommender.
manifest() {
  local namespace=$1 name=$2 uid=$3 heuristic=$4 metadata spec
  case "$ENVIRONMENT/$heuristic" in
  tiny-vpa/sma) metadata="" spec="  recommenders: [{name: tiny}]" ;;
  tiny-vpa/vpa) metadata="  annotations: {vhape/policy: vpa-policy}" spec="" ;;
  vhape/*)
    metadata="  labels: {app.kubernetes.io/managed-by: vhape-watcher, autoscaling.vhape.io/recommender: vhape-recommender}
  annotations: {vhape/policy: $heuristic-policy}"
    spec="  recommenders: [{name: vhape-recommender}]"
    ;;
  esac
  cat <<EOF
---
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: $name
  namespace: $namespace
$metadata
  ownerReferences:
  - {apiVersion: apps/v1, kind: Deployment, name: $name, uid: $uid, controller: true}
spec:
$spec
  targetRef: {apiVersion: apps/v1, kind: Deployment, name: $name}
  updatePolicy: {updateMode: "Off"}
  resourcePolicy:
    containerPolicies:
    - containerName: "*"
      controlledValues: RequestsOnly
EOF
}

log "creating objects with updateMode Off for the Deployments of the namespaces matching '$REGEX' ($ENVIRONMENT," \
  "$(wc -w <<<"$listed") Deployments listed for sma)"
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
      case "$listed" in *" $namespace/$name "*) heuristic=sma ;; *) heuristic=vpa ;; esac
      batch+="$(manifest "$namespace" "$name" "$uid" "$heuristic")"$'\n'
      sent+="$namespace/$name/$heuristic "
    done <<<"$deployments"
    if [[ -n $batch ]]; then
      # kubectl creates each object on its own, so one failure does not stop the others.
      kubectl apply -f - <<<"$batch" >/dev/null 2>"$errors" ||
        log "error creating objects, retrying: $(tr '\n' ' ' <"$errors")"
      # The objects are listed again to log the ones created; the missing ones are retried on the next check.
      if objects=$(list verticalpodautoscalers NAMESPACE:.metadata.namespace,NAME:.metadata.name); then
        existing=" $(awk '{printf "%s/%s ", $1, $2}' <<<"$objects")"
        for object in $sent; do
          case "$existing" in *" ${object%/*} "*) log "created ${object%/*} (${object##*/})" ;; esac
        done
      fi
    fi
  fi
  sleep "$CHECK_INTERVAL"
done
