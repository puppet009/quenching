#!/usr/bin/env bash
# Existing platform release only: preserve its secrets, data, replica/resource values.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
NAMESPACE="${NAMESPACE:-platform}"
TAG="${IMAGE_TAG:-0.1.0-alpha.1}"
helm status quenching -n "$NAMESPACE" >/dev/null
bash "$SCRIPT_DIR/prepare-logs.sh"
# Fail before changing running workloads if image/config validation fails.
IMAGE_TAG="$TAG" bash "$SCRIPT_DIR/images.sh"
helm template quenching "$REPO_ROOT/deploy/helm" -n "$NAMESPACE" \
  --set logging.persistence.enabled=true --set logging.collector.enabled=true \
  --set-string logging.persistence.hostPath="$REPO_ROOT/logs" >/dev/null

# ALTER SYSTEM statements must be separate -c invocations (no transaction block).
collector_before="$(kubectl -n "$NAMESPACE" exec postgresql-0 -c postgresql -- psql -U quenching -d quenching -Atc 'SHOW logging_collector')"
timezone_before="$(kubectl -n "$NAMESPACE" get statefulset postgresql -o jsonpath='{.spec.template.spec.containers[?(@.name=="postgresql")].env[?(@.name=="TZ")].value}')"
kubectl -n "$NAMESPACE" exec postgresql-0 -c postgresql -- psql -U quenching -d quenching -v ON_ERROR_STOP=1 \
  -c "ALTER SYSTEM SET timezone TO 'Asia/Shanghai'" \
  -c "ALTER SYSTEM SET log_timezone TO 'Asia/Shanghai'" \
  -c "ALTER SYSTEM SET log_destination TO 'stderr'" \
  -c "ALTER SYSTEM SET logging_collector TO 'off'" \
  -c 'SELECT pg_reload_conf()'
if [[ "$timezone_before" != Asia/Shanghai ]]; then
  kubectl -n "$NAMESPACE" set env statefulset/postgresql TZ=Asia/Shanghai
elif [[ "$collector_before" == on ]]; then
  kubectl -n "$NAMESPACE" rollout restart statefulset/postgresql
fi
kubectl -n "$NAMESPACE" rollout status statefulset/postgresql --timeout=5m
database_configured=f
for attempt in {1..10}; do
  database_configured="$(kubectl -n "$NAMESPACE" exec postgresql-0 -c postgresql -- psql -U quenching -d quenching -Atc "SELECT current_setting('TimeZone')='Asia/Shanghai' AND current_setting('log_timezone')='Asia/Shanghai' AND current_setting('logging_collector')='off' AND current_setting('log_destination')='stderr'")"
  [[ "$database_configured" == t ]] && break
  sleep 1
done
if [[ "$database_configured" != t ]]; then
  echo "数据库时间/日志配置尚未生效，请检查现有启动参数或角色、数据库时区覆盖；停止升级。" >&2
  exit 1
fi

helm upgrade quenching "$REPO_ROOT/deploy/helm" -n "$NAMESPACE" \
  --reset-then-reuse-values --set-string global.imageTag="$TAG" \
  --set-string global.timezone=Asia/Shanghai \
  --set logging.persistence.enabled=true --set logging.collector.enabled=true \
  --set-string logging.persistence.hostPath="$REPO_ROOT/logs" --wait --timeout=10m
kubectl -n "$NAMESPACE" set env deployment/web deployment/admin TZ=Asia/Shanghai
for frontend in web admin; do
  current_image="$(kubectl -n "$NAMESPACE" get deployment "$frontend" -o "jsonpath={.spec.template.spec.containers[?(@.name==\"$frontend\")].image}")"
  image_repository="${current_image%@*}"
  if [[ "${image_repository##*/}" == *:* ]]; then image_repository="${image_repository%:*}"; fi
  kubectl -n "$NAMESPACE" set image deployment/"$frontend" "$frontend=$image_repository:$TAG"
done
for service in api-gateway content-service user-service training-service review-service fault-engine sandbox-service ai-service web admin; do
  kubectl -n "$NAMESPACE" rollout restart deployment/"$service"
  kubectl -n "$NAMESPACE" rollout status deployment/"$service" --timeout=5m
done
kubectl -n "$NAMESPACE" rollout restart daemonset/quenching-log-agent
kubectl -n "$NAMESPACE" rollout status daemonset/quenching-log-agent --timeout=5m
kubectl -n "$NAMESPACE" exec postgresql-0 -c postgresql -- psql -U quenching -d quenching \
  -c 'SHOW timezone' -c 'SHOW log_timezone' -c 'SHOW logging_collector' -c 'SELECT now()'
echo "升级完成，日志目录: $REPO_ROOT/logs"
