#!/usr/bin/env bash
# Run the real deploy runner in a temporary repository; never contact a cluster.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
temp_parent="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
task_tmp="$(mktemp -d "$temp_parent/quenching-deploy-version.XXXXXX")"
task_tmp="$(cd "$task_tmp" && pwd -P)"
[[ -d "$task_tmp" && "$task_tmp" == "$temp_parent"/quenching-deploy-version.* ]]
trap 'rm -rf -- "$task_tmp"' EXIT
fixture="$task_tmp/repo/deploy/server"
mkdir -p "$fixture/manifests" "$task_tmp/repo/deploy/helm" "$task_tmp/bin"
cp "$root/deploy/server/deploy.sh" "$fixture/deploy.sh"
cp "$root/deploy/server/values-server.yaml" "$fixture/values-server.yaml"
cp "$root/deploy/server/manifests/"{00-namespace,15-gateway,20-frontend}.yaml "$fixture/manifests/"
export DEPLOY_TEST_TRACE="$task_tmp/trace"
export DEPLOY_TEST_HELM_ARGS="$task_tmp/helm-args"
export DEPLOY_TEST_FRONTEND="$task_tmp/frontend.yaml"
export KUBECONFIG="$task_tmp/kubeconfig"
export JWT_SECRET=offline-test-jwt INTERNAL_TOKEN=offline-test-internal
touch "$KUBECONFIG"
cat > "$fixture/prepare-logs.sh" <<'STUB'
#!/usr/bin/env bash
echo prepare-logs >> "$DEPLOY_TEST_TRACE"
STUB
cat > "$task_tmp/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
echo "kubectl $*" >> "$DEPLOY_TEST_TRACE"
if [[ "$1" == version ]]; then
  [[ "${DEPLOY_TEST_FAIL:-}" != preflight ]]
elif [[ "$1" == apply && "$2" == -f ]]; then
  if [[ "$3" == - ]]; then
    cat > "$DEPLOY_TEST_FRONTEND"
    [[ "${DEPLOY_TEST_FAIL:-}" != frontend ]]
  elif [[ "$3" == */20-frontend.yaml ]]; then
    cat "$3" > "$DEPLOY_TEST_FRONTEND"
  else
    [[ -f "$3" ]]
  fi
elif [[ "$1" == --namespace && "$3" == get ]]; then
  :
else
  echo "unexpected kubectl invocation: $*" >&2
  exit 1
fi
STUB
cat > "$task_tmp/bin/helm" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
echo "helm $*" >> "$DEPLOY_TEST_TRACE"
printf '%s\n' "$@" > "$DEPLOY_TEST_HELM_ARGS"
[[ "${DEPLOY_TEST_FAIL:-}" != helm ]]
STUB
chmod +x "$task_tmp/bin/"*
export PATH="$task_tmp/bin:$PATH"

run_deploy() {
  local tag="$1"
  shift
  : > "$DEPLOY_TEST_TRACE"
  : > "$DEPLOY_TEST_HELM_ARGS"
  rm -f -- "$DEPLOY_TEST_FRONTEND"
  if [[ "$tag" == DEFAULT ]]; then
    env -u IMAGE_TAG bash "$fixture/deploy.sh" "$@" > "$task_tmp/output" 2>&1
  else
    IMAGE_TAG="$tag" bash "$fixture/deploy.sh" "$@" > "$task_tmp/output" 2>&1
  fi
}
assert_helm_tag() {
  grep -A1 -x -- '--set-string' "$DEPLOY_TEST_HELM_ARGS" \
    | grep -Fx -- "global.imageTag=$1" >/dev/null || {
      echo "Helm did not receive image tag $1 with --set-string" >&2
      exit 1
    }
}
assert_frontend_images() {
  printf 'quenching/web:%s\nquenching/admin:%s\n' "$1" "$1" > "$task_tmp/expected-images"
  awk '$1 == "image:" {print $2}' "$DEPLOY_TEST_FRONTEND" > "$task_tmp/actual-images"
  diff -u "$task_tmp/expected-images" "$task_tmp/actual-images"
  [[ "$(grep -c '^kubectl apply -f -$' "$DEPLOY_TEST_TRACE")" == 1 ]]
}

run_deploy 0.1.0-alpha.7
assert_helm_tag 0.1.0-alpha.7
assert_frontend_images 0.1.0-alpha.7
diff -u "$root/deploy/server/manifests/20-frontend.yaml" "$fixture/manifests/20-frontend.yaml"

run_deploy DEFAULT
assert_helm_tag 0.1.0-alpha.1
assert_frontend_images 0.1.0-alpha.1

run_deploy 0.1.0-alpha.7 --no-expose
assert_helm_tag 0.1.0-alpha.7
[[ ! -e "$DEPLOY_TEST_FRONTEND" ]]
! grep -F '15-gateway.yaml' "$DEPLOY_TEST_TRACE"

for invalid in 'bad/tag' 'bad&tag' 'bad tag' '-alpha' '.alpha' 'alpha:tag' "$(printf '%129s' '' | tr ' ' a)"; do
  if run_deploy "$invalid"; then
    echo "deploy accepted invalid image tag: $invalid" >&2
    exit 1
  fi
  [[ ! -s "$DEPLOY_TEST_TRACE" ]]
done

if DEPLOY_TEST_FAIL=preflight run_deploy 0.1.0-alpha.7; then
  echo 'deploy ignored failed cluster preflight' >&2
  exit 1
fi
[[ ! -s "$DEPLOY_TEST_HELM_ARGS" ]]
! grep -F 'kubectl apply' "$DEPLOY_TEST_TRACE"

if DEPLOY_TEST_FAIL=helm run_deploy 0.1.0-alpha.7; then
  echo 'deploy ignored failed Helm upgrade' >&2
  exit 1
fi
[[ ! -e "$DEPLOY_TEST_FRONTEND" ]]
! grep -F '15-gateway.yaml' "$DEPLOY_TEST_TRACE"

if DEPLOY_TEST_FAIL=frontend run_deploy 0.1.0-alpha.7; then
  echo 'deploy ignored failed frontend apply' >&2
  exit 1
fi
! grep -F 'get svc' "$DEPLOY_TEST_TRACE"
! grep -F 'get pods' "$DEPLOY_TEST_TRACE"

echo 'PASS: release/default tags reach Helm and frontend on first apply; --no-expose, invalid tags, and failed dependencies stop safely'
