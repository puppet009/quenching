#!/usr/bin/env bash
# ============================================================================
# migrate.sh  生成并执行六个服务的数据库迁移 Job
#
# 迁移文件来自各服务镜像内 /app/migrations；数据库为 platform 命名空间的
# postgresql:5432（库/用户 quenching）。每个服务使用独立迁移表，
# 规则与仓库根 Makefile 一致：schema_migrations_<目录名，中划线转下划线>
#
# 用法：
#   ./migrate.sh            # 生成清单 -> apply -> 等待完成
#   ./migrate.sh render     # 只输出 YAML（不 apply）
#   ./migrate.sh clean      # 删除旧迁移 Job（重跑前使用）
# ============================================================================
set -euo pipefail

NAMESPACE="${NAMESPACE:-platform}"
IMAGE_TAG="${IMAGE_TAG:-0.1.0-alpha.1}"
DB_DSN="${DB_DSN:-postgres://quenching:quenching@postgresql:5432/quenching?sslmode=disable}"
# 必须是 services/ 下的目录名：镜像名即 quenching/<目录名>，
# 迁移表 = schema_migrations_<目录名中划线转下划线>。
# Historical configuration and evidence sources must exist before ownership
# handoff migrations in content/sandbox, including on a fresh database.
SERVICES=(fault-engine training-service user-service \
          content-service sandbox-service review-service)

render() {
  local targets=("${SERVICES[@]}")
  if (( $# > 0 )); then targets=("$@"); fi
  for svc in "${targets[@]}"; do
    table="schema_migrations_${svc//-/_}"
    # v4.17.1 CLI 用标准 flag 包，不识别 -table / -x-migrations-table 命令行参数，
    # 传了会报 "flag provided but not defined" 并打印用法退出。
    # 自定义迁移表只能通过 postgres URL 查询参数 x-migrations-table 传给驱动
    # （口径同仓库根 Makefile）。按基础 DSN 是否已有查询串选分隔符。
    if [[ "${DB_DSN}" == *\?* ]]; then sep="&"; else sep="?"; fi
    svc_dsn="${DB_DSN}${sep}x-migrations-table=${table}"
    cat <<YAML
---
apiVersion: batch/v1
kind: Job
metadata:
  name: migrate-${svc}
  namespace: ${NAMESPACE}
  labels:
    app: quenching-migration
    service: ${svc}
spec:
  backoffLimit: 2
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        app: quenching-migration
        service: ${svc}
    spec:
      restartPolicy: Never
      initContainers:
        - name: fetch
          image: quenching/${svc}:${IMAGE_TAG}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-c", "cp -r /app/migrations/. /migrations/"]
          volumeMounts:
            - { name: migrations, mountPath: /migrations }
      containers:
        - name: migrate
          image: migrate/migrate:v4.17.1
          imagePullPolicy: IfNotPresent
          # 该镜像为 scratch 基底（无 shell），直接把参数作为 entrypoint 入参。
          args:
            - -path=/migrations
            - -database=${svc_dsn}
            - up
          volumeMounts:
            - { name: migrations, mountPath: /migrations }
      volumes:
        - name: migrations
          emptyDir: {}
YAML
  done
}

case "${1:-apply}" in
  render)
    render
    ;;
  clean)
    kubectl -n "${NAMESPACE}" delete job -l app=quenching-migration --ignore-not-found
    ;;
  apply)
    for svc in "${SERVICES[@]}"; do
      render "$svc" | kubectl apply -f -
      echo "等待 migrate-${svc} 完成 ..."
      kubectl -n "${NAMESPACE}" wait --for=condition=complete \
        "job/migrate-${svc}" --timeout=300s
    done
    kubectl -n "${NAMESPACE}" get jobs -l app=quenching-migration
    ;;
  *)
    echo "用法: $0 [apply|render|clean]" >&2
    exit 1
    ;;
esac
