#!/usr/bin/env bash
# Read-only server evidence check. Does not create accounts, inject faults or call a paid model.
set -euo pipefail
: "${SESSION_ID:?set SESSION_ID}"
: "${PLATFORM_URL:?set gateway base URL}"
: "${USER_ACCESS_TOKEN:?set session owner access token}"
[[ "$SESSION_ID" =~ ^[1-9][0-9]*$ ]] || { echo 'FAIL invalid session'; exit 1; }
status=$(curl --fail --silent --show-error --header "Authorization: Bearer ${USER_ACCESS_TOKEN}" "${PLATFORM_URL%/}/api/v1/training/sessions/${SESSION_ID}/brain")
printf '%s' "$status" | python3 -c 'import json,sys; x=json.load(sys.stdin); assert x["code"]==0; d=x["data"]; print(json.dumps({k:d[k] for k in ("phase","current_challenge","cleanup_status","ability_observed")},ensure_ascii=False))'
if [[ "${EXPECT_CLOSED:-false}" == true ]]; then
  printf '%s' "$status" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; assert d["cleanup_status"]=="succeeded" and d["phase"]=="completed", "closure is not complete"'
  remaining=$(kubectl get namespace "sandbox-${SESSION_ID}" --ignore-not-found -o name)
  [[ -z "$remaining" ]] || { echo 'FAIL namespace remains'; exit 1; }
  echo 'PASS reported closure and observed namespace absence; inspect PV deletion and retained review separately'
else
  kubectl -n "sandbox-${SESSION_ID}" get pods -o wide
  kubectl -n "sandbox-${SESSION_ID}" get pvc
  echo 'OBSERVED runtime state only; this does not certify business recovery or fault acceptance'
fi
