#!/usr/bin/env bash
# Exercise the real migration runner with a fake kubectl; no cluster access.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
task_tmp="$(mktemp -d)"
trap 'rm -rf -- "$task_tmp"' EXIT
export MIGRATION_TRACE="$task_tmp/trace"
cat > "$task_tmp/kubectl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == apply ]]; then
  name="$(awk '/^  name: migrate-/{print $2}')"
  echo "apply $name" >> "$MIGRATION_TRACE"
elif [[ "$*" == *" wait "* ]]; then
  for arg in "$@"; do
    if [[ "$arg" == job/migrate-* ]]; then
      echo "wait ${arg#job/}" >> "$MIGRATION_TRACE"
      [[ "${FAIL_FIRST_WAIT:-0}" != 1 ]] || exit 1
      exit 0
    fi
  done
  echo 'wait ALL' >> "$MIGRATION_TRACE"
fi
MOCK
chmod +x "$task_tmp/kubectl"
export PATH="$task_tmp:$PATH"
bash "$root/deploy/server/migrate.sh" > "$task_tmp/output"
for svc in fault-engine training-service user-service content-service sandbox-service review-service; do
  echo "apply migrate-$svc"
  echo "wait migrate-$svc"
done > "$task_tmp/expected"
diff -u "$task_tmp/expected" "$MIGRATION_TRACE"
: > "$MIGRATION_TRACE"
export FAIL_FIRST_WAIT=1
if bash "$root/deploy/server/migrate.sh" > "$task_tmp/output"; then
  echo 'runner ignored failed dependency' >&2; exit 1
fi
printf '%s\n' 'apply migrate-fault-engine' 'wait migrate-fault-engine' > "$task_tmp/expected"
diff -u "$task_tmp/expected" "$MIGRATION_TRACE"
[[ "$(bash "$root/deploy/server/migrate.sh" render | awk '/^kind: Job$/{n++}END{print n}')" == 6 ]]
echo 'PASS: ordered migration jobs, dependency failure stops runner, render emits six jobs'
