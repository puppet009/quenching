#!/usr/bin/env bash
# Prepare only <repository>/logs for the non-root backend UID/GID (10001).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
LOG_ROOT="${REPO_ROOT}/logs"

if [[ -L "${LOG_ROOT}" || ( -e "${LOG_ROOT}" && ! -d "${LOG_ROOT}" ) ]]; then
  echo "日志目录必须是仓库内的真实目录: ${LOG_ROOT}" >&2
  exit 1
fi
if [[ "$(id -u)" != 0 ]]; then
  echo "请用 sudo bash ${SCRIPT_DIR}/prepare-logs.sh 为容器用户准备日志目录。" >&2
  exit 1
fi
install -d -m 2775 -o 10001 -g 10001 -- "${LOG_ROOT}"
echo "日志目录已准备: ${LOG_ROOT} (uid/gid=10001)"
