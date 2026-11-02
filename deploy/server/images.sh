#!/usr/bin/env bash
# ============================================================================
# images.sh  构建全部平台镜像（共 11 个，tag 默认 0.1.0-alpha.1）
#
#   7 个 Go 服务：api-gateway / content / user / training / review /
#                 fault-engine / sandbox（仓库根上下文）
#   1 个 Python：ai-service（services/ai-service 上下文）
#   2 个前端：  web / admin（仓库根上下文）
#   1 个基础设施采集器：log-agent（仓库根上下文）
#
# 构建出的镜像直接进入本机 containerd（k3s 使用）。
# 单节点无需推镜像仓库；如要迁移到别的节点，参考 README 的 save/load。
#
# 用法：
#   ./images.sh              # 构建全部
#   ./images.sh web admin    # 只构建指定目标
#   NOCACHE=1 ./images.sh    # 不用层缓存
# ============================================================================
set -euo pipefail

TAG="${IMAGE_TAG:-0.1.0-alpha.1}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NOCACHE_FLAG=""
[ "${NOCACHE:-0}" = "1" ] && NOCACHE_FLAG="--no-cache"
GOPROXY_URL="${GOPROXY:-https://goproxy.cn,direct}"
# 华为云 helm 源目录含版本子目录（与 get.helm.sh 不同），需与 Dockerfile 的 HELM_VERSION 对齐
HELM_MIRROR_URL="${HELM_MIRROR:-https://mirrors.huaweicloud.com/helm/v3.16.3/}"

log() { printf '\n\033[1;36m[images]\033[0m %s\n' "$*"; }

# docker / nerdctl / k3s ctr 的统一封装：构建后确保进 k3s containerd。
# 本脚本默认使用 docker build；docker 装在宿主机时镜像在 docker daemon，
# 需通过 k3s ctr 导入（见 README），单机推荐直接用 nerdctl（见函数末尾说明）。
build_root() { # 名称  Dockerfile 相对路径
  local name="$1" dockerfile="$2"
  local extra_args=()
  if [ "$name" = "sandbox-service" ]; then
    extra_args=(--build-arg "HELM_MIRROR=${HELM_MIRROR_URL}")
  fi
  log "构建 quenching/${name}:${TAG}（上下文：仓库根）"
  if [ "${#extra_args[@]}" -gt 0 ]; then
    docker build ${NOCACHE_FLAG} "${extra_args[@]}" \
      -f "${REPO_ROOT}/${dockerfile}" \
      -t "quenching/${name}:${TAG}" \
      "${REPO_ROOT}"
  else
    docker build ${NOCACHE_FLAG} \
      -f "${REPO_ROOT}/${dockerfile}" \
      -t "quenching/${name}:${TAG}" \
      "${REPO_ROOT}"
  fi
}

build_go()      { build_root "$1" "services/$1/Dockerfile"; }
build_web()     { build_root web   "apps/web/Dockerfile"; }
build_admin()   { build_root admin "apps/admin/Dockerfile"; }
build_ai() {
  log "构建 quenching/ai-service:${TAG}（上下文：services/ai-service）"
  docker build ${NOCACHE_FLAG} \
    -t "quenching/ai-service:${TAG}" \
    "${REPO_ROOT}/services/ai-service"
}

ALL_TARGETS=(api-gateway content-service user-service training-service \
             review-service fault-engine sandbox-service ai-service web admin log-agent)

TARGETS=("$@")
[ "${#TARGETS[@]}" -eq 0 ] && TARGETS=("${ALL_TARGETS[@]}")

for t in "${TARGETS[@]}"; do
  case "$t" in
    api-gateway|content-service|user-service|training-service|\
    review-service|fault-engine|sandbox-service) build_go "$t" ;;
    ai-service) build_ai ;;
    log-agent) build_root log-agent "deploy/log-agent/Dockerfile" ;;
    web) build_web ;;
    admin) build_admin ;;
    *) echo "未知目标: $t" >&2; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# 导入 k3s：docker daemon 与 k3s containerd 相互独立，必须显式导入，
# 否则 Pod 会报 ImagePullBackOff。
# ---------------------------------------------------------------------------
log "把镜像导入 k3s containerd ..."
for t in "${TARGETS[@]}"; do
  docker save "quenching/${t}:${TAG}" \
    | k3s ctr images import - --no-unpack
done

log "完成。k3s 已就绪镜像："
k3s ctr images ls | grep "quenching/" | grep "${TAG}" || true
