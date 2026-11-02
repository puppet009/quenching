#!/usr/bin/env bash
# Build the pinned template and preload all images into the local k3s node.
set -euo pipefail
TEMPLATE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAG="${MALL_IMAGE_TAG:-mall-0.2.0}"
IMPORT_K3S="${IMPORT_K3S:-1}"

command -v docker >/dev/null || { echo 'docker is required on the build server' >&2; exit 1; }
docker info >/dev/null
if [[ "$IMPORT_K3S" == 1 ]]; then
  command -v k3s >/dev/null || { echo 'k3s is required, or set IMPORT_K3S=0 for build-only' >&2; exit 1; }
  k3s ctr images ls >/dev/null
fi

images=()
for component in portal frontend mysql; do
  image="quenching/mall-${component}:${TAG}"
  docker build -f "${TEMPLATE_ROOT}/docker/${component}.Dockerfile" -t "$image" "$TEMPLATE_ROOT"
  images+=("$image")
done

# Keep this list in sync with helm/values.yaml. These are runtime dependencies,
# not app build artifacts; importing avoids cold pulls during sandbox startup.
for image in redis:7.4.2-alpine rabbitmq:3.13.7-alpine mongo:7.0.16 busybox:1.37.0; do
  docker pull "$image"
  images+=("$image")
done

if [[ "$IMPORT_K3S" == 1 ]]; then
  for image in "${images[@]}"; do
    docker save "$image" | k3s ctr images import -
  done
fi
printf 'Template images ready; tag=%s. See SERVER_DEPLOY.md for platform updates.\n' "$TAG"
