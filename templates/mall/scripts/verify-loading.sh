#!/usr/bin/env bash
# Read-only runtime checks after the PLATFORM creates a mall sandbox.
set -euo pipefail
NS="${1:?usage: bash scripts/verify-loading.sh sandbox-SESSION_ID}"
[[ "$NS" =~ ^sandbox-[0-9]+$ ]] || { echo 'Use a platform-created sandbox namespace' >&2; exit 1; }
kubectl get networkpolicy sandbox-isolation -n "$NS" >/dev/null
kubectl get resourcequota sandbox-quota -n "$NS" >/dev/null
kubectl wait -n "$NS" --for=condition=Available --timeout=300s deployment --all
kubectl get pods,svc,pvc -n "$NS"

kubectl exec -n "$NS" deployment/frontend -- wget -q -O - http://127.0.0.1/healthz
kubectl exec -n "$NS" deployment/frontend -- wget -q -O - http://mall-portal:8085/actuator/health
home="$(kubectl exec -n "$NS" deployment/frontend -- wget -q -O - http://127.0.0.1/api/home/content)"
grep -Eq '"code"[[:space:]]*:[[:space:]]*200' <<< "$home" || { echo 'Storefront home API did not return business success' >&2; exit 1; }

for url in http://127.0.0.1/api/order/paySuccess http://mall-portal:8085/order/paySuccess http://mall-portal:8085/alipay/webPay; do
  response="$(kubectl exec -n "$NS" deployment/frontend -- wget --spider -S "$url" 2>&1 || true)"
  if ! grep -q 'HTTP/1\.[01] 503' <<< "$response"; then
    echo "Expected disabled payment response at $url" >&2
    exit 1
  fi
done
printf '\nLoading checks passed. Full training admission is still pending.\n'
