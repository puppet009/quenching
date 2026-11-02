#!/usr/bin/env bash
# ============================================================================
# 01-bootstrap.sh  单机环境初始化（火山引擎 8C16G + 100G 数据盘）
#
# 幂等：已完成的步骤可安全重跑。生产首装时建议仍按手册逐条执行并观察输出。
# 完成内容：
#   1. 数据盘分区/格式化/挂载 /data，并写入 /etc/fstab
#   2. 安装 k3s（数据目录 /data/k3s，国内镜像，关闭自带 Traefik）
#   3. 配置 containerd docker.io 加速（DaoCloud）
#   4. 安装 helm（华为云镜像）
#
# 变量按现场实际修改：
set -euo pipefail

DATA_DISK="${DATA_DISK:-/dev/vdb}"          # 数据盘设备名
DATA_DIR="${DATA_DIR:-/data}"               # 数据盘挂载点
K3S_VERSION="${K3S_VERSION:-v1.36.4+k3s1}"  # 与已验证版本保持一致
HELM_VERSION="${HELM_VERSION:-v3.16.3}"

log() { printf '\n\033[1;32m[bootstrap]\033[0m %s\n' "$*"; }

# ---------------------------------------------------------------------------
# 1. 数据盘
# ---------------------------------------------------------------------------
if ! mountpoint -q "${DATA_DIR}"; then
  log "初始化数据盘 ${DATA_DISK} -> ${DATA_DIR}"
  if ! lsblk -ndo TYPE "${DATA_DISK}1" 2>/dev/null | grep -q part; then
    parted -s "${DATA_DISK}" mklabel gpt
    parted -s "${DATA_DISK}" mkpart primary ext4 0% 100%
  fi
  PART="${DATA_DISK}1"
  # 未格式化才格式化（避免误清数据）
  if ! blkid -s TYPE -o value "${PART}" | grep -q ext4; then
    mkfs.ext4 -F "${PART}"
  fi
  mkdir -p "${DATA_DIR}"
  mount "${PART}" "${DATA_DIR}"
  UUID="$(blkid -s UUID -o value "${PART}")"
  grep -v "^UUID=${UUID} " /etc/fstab > /tmp/fstab.new || true
  echo "UUID=${UUID} ${DATA_DIR} ext4 defaults 0 2" >> /tmp/fstab.new
  cat /tmp/fstab.new > /etc/fstab
else
  log "${DATA_DIR} 已挂载，跳过数据盘初始化"
fi
df -h "${DATA_DIR}"

# ---------------------------------------------------------------------------
# 2. k3s
# ---------------------------------------------------------------------------
if ! command -v kubectl >/dev/null 2>&1; then
  log "安装 k3s ${K3S_VERSION}（数据目录 ${DATA_DIR}/k3s）"
  curl -fsSL https://rancher-mirror.rancher.cn/k3s/k3s-install.sh -o /tmp/k3s-install.sh
  INSTALL_K3S_MIRROR=cn INSTALL_K3S_SKIP_START=false \
  INSTALL_K3S_VERSION="${K3S_VERSION}" \
  sh /tmp/k3s-install.sh \
    --data-dir "${DATA_DIR}/k3s" \
    --write-kubeconfig-mode 644 \
    --disable traefik
else
  log "k3s 已安装：$(kubectl version 2>/dev/null | grep 'Server Version' || true)"
fi

# ---------------------------------------------------------------------------
# 3. containerd 镜像加速
# ---------------------------------------------------------------------------
log "配置 containerd docker.io 加速（DaoCloud）"
mkdir -p /etc/rancher/k3s
cat > /etc/rancher/k3s/registries.yaml <<'YAML'
mirrors:
  docker.io:
    endpoint:
      - "https://docker.m.daocloud.io"
YAML
systemctl restart k3s

# ---------------------------------------------------------------------------
# 4. helm
# ---------------------------------------------------------------------------
if ! command -v helm >/dev/null 2>&1; then
  log "安装 helm ${HELM_VERSION}（华为云镜像）"
  ARCH="$(uname -m)"
  case "${ARCH}" in
    x86_64) HARCH=linux-amd64 ;;
    aarch64) HARCH=linux-arm64 ;;
    *) echo "unsupported arch: ${ARCH}" >&2; exit 1 ;;
  esac
  curl -fsSL "https://mirrors.huaweicloud.com/helm/${HELM_VERSION}/helm-${HELM_VERSION}-${HARCH}.tar.gz" -o /tmp/helm.tgz
  tar -xzf /tmp/helm.tgz -C /tmp
  mv "/tmp/${HARCH}/helm" /usr/local/bin/helm
else
  log "helm 已安装：$(helm version --short)"
fi

# 让 helm（及其他只读标准 kubeconfig 的工具）能找到集群：
# k3s 自带的 kubectl 会自动读 /etc/rancher/k3s/k3s.yaml，但标准 helm 不会。
KUBECONF="${HOME}/.kube/config"
if [[ ! -f "${KUBECONF}" && -f /etc/rancher/k3s/k3s.yaml ]]; then
  log "复制 k3s kubeconfig 到 ${KUBECONF}"
  mkdir -p "${HOME}/.kube"
  cp /etc/rancher/k3s/k3s.yaml "${KUBECONF}"
  chmod 600 "${KUBECONF}"
fi

log "完成。等待节点 Ready 与系统 Pod Running ..."
kubectl wait --for=condition=Ready node --all --timeout=120s
kubectl get pods -A
