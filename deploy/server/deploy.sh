#!/usr/bin/env bash
# 单机一键部署：安装/升级 quenching helm release，并暴露网关与前端。
# 密钥在脚本内部生成，避免手工粘贴多行命令时引号/空格被终端改写。
#
# 用法（在仓库根目录或任意目录执行均可）：
#   bash deploy/server/deploy.sh            # 安装 + 暴露 30080/30081/30082
#   bash deploy/server/deploy.sh --no-expose  # 只装 helm，不 apply 网关/前端清单
#
# 可用环境变量覆盖：
#   NAMESPACE=platform  IMAGE_TAG=0.1.0-alpha.1
#   JWT_SECRET=xxx      INTERNAL_TOKEN=xxx   # 不提供则随机生成
set -euo pipefail

EXPOSE=1
for arg in "$@"; do
  case "$arg" in
    --no-expose) EXPOSE=0 ;;
    *) echo "未知参数: $arg" >&2; exit 2 ;;
  esac
done

IMAGE_TAG="${IMAGE_TAG:-0.1.0-alpha.1}"
if [[ ! "$IMAGE_TAG" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$ ]]; then
  echo "无效的 IMAGE_TAG: ${IMAGE_TAG}（须为 1–128 位合法镜像标签）" >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FRONTEND_MANIFEST=""
if [[ "$EXPOSE" == "1" ]]; then
  FRONTEND_MANIFEST="$(sed -E \
    "s#(^[[:space:]]*image:[[:space:]]*quenching/(web|admin):)[^[:space:]]+#\\1${IMAGE_TAG}#" \
    "${SCRIPT_DIR}/manifests/20-frontend.yaml")"
fi

NAMESPACE="${NAMESPACE:-platform}"
HELM_TIMEOUT="${HELM_TIMEOUT:-10m}"
JWT_SECRET="${JWT_SECRET:-$(openssl rand -hex 32)}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-$(openssl rand -hex 32)}"

# kubeconfig：k3s 的 kubectl 会自动读 /etc/rancher/k3s/k3s.yaml，
# 但标准 helm 只认 ~/.kube/config 或 KUBECONFIG，缺省时会退回 localhost:8080。
# 这里按顺序探测并导出，保证 kubectl/helm 用同一份配置。
if [[ -z "${KUBECONFIG:-}" ]]; then
  for candidate in "${HOME}/.kube/config" /etc/rancher/k3s/k3s.yaml; do
    if [[ -f "${candidate}" ]]; then
      export KUBECONFIG="${candidate}"
      break
    fi
  done
fi
if [[ -z "${KUBECONFIG:-}" || ! -f "${KUBECONFIG}" ]]; then
  echo "找不到 kubeconfig（试过 ~/.kube/config 与 /etc/rancher/k3s/k3s.yaml）。" >&2
  echo "请先 export KUBECONFIG=/path/to/kubeconfig" >&2
  exit 1
fi

echo "==> 仓库目录: ${REPO_ROOT}"
echo "==> 命名空间: ${NAMESPACE}"
echo "==> kubeconfig: ${KUBECONFIG}"

# 连通性预检（kubeconfig 不对时立即给出清晰报错，而不是等 helm 退回 localhost:8080）
if ! kubectl version --request-timeout=5s >/dev/null 2>&1; then
  echo "kubectl 无法连接集群，请检查 KUBECONFIG=${KUBECONFIG}" >&2
  exit 1
fi

# 命名空间（00-namespace.yaml 已存在则 apply，幂等）
kubectl apply -f "${REPO_ROOT}/deploy/server/manifests/00-namespace.yaml"

echo "==> 安装/升级 helm release: quenching（等待超时 ${HELM_TIMEOUT}）"
bash "${SCRIPT_DIR}/prepare-logs.sh"
helm upgrade --install quenching "${REPO_ROOT}/deploy/helm" \
  --namespace "${NAMESPACE}" \
  -f "${REPO_ROOT}/deploy/server/values-server.yaml" \
  --set-string global.imageTag="${IMAGE_TAG}" \
  --set-string logging.persistence.hostPath="${REPO_ROOT}/logs" \
  --set global.jwtSecret="${JWT_SECRET}" \
  --set observability.wsGatewayApiKey="${INTERNAL_TOKEN}" \
  --timeout "${HELM_TIMEOUT}" \
  --wait

echo
echo "==> 本轮密钥（请妥善保存，重装时可用环境变量传入以保持不变）："
echo "    JWT_SECRET=${JWT_SECRET}"
echo "    INTERNAL_TOKEN=${INTERNAL_TOKEN}"

if [[ "${EXPOSE}" == "1" ]]; then
  echo
  echo "==> 暴露网关 (30082) 与前端 web/admin (30080/30081)"
  kubectl apply -f "${REPO_ROOT}/deploy/server/manifests/15-gateway.yaml"
  printf '%s\n' "$FRONTEND_MANIFEST" | kubectl apply -f -

  echo
  echo "==> ${NAMESPACE} 命名空间 Service 列表："
  kubectl --namespace "${NAMESPACE}" get svc
fi

echo
echo "==> 完成。Pod 状态："
kubectl --namespace "${NAMESPACE}" get pods
