#!/usr/bin/env bash
# Keep PID 1 alive while the student's application is stopped for a rebuild.
set -euo pipefail

source_dir=${TRAINING_SOURCE_DIR:-/workspace/mall}
runtime_dir=${TRAINING_RUNTIME_DIR:-/workspace/runtime}
source_seed=${TRAINING_SOURCE_SEED:-/opt/training/mall}
runtime_seed=${TRAINING_RUNTIME_SEED:-/opt/training/runtime}
app_dir=${TRAINING_APP_DIR:-/app}
state_dir=${TRAINING_STATE_DIR:-/run/training}
stop_timeout=${TRAINING_STOP_TIMEOUT:-10}
child_pid=

mkdir -p "$source_dir" "$runtime_dir" "$state_dir"
# The immutable seeds live outside the workspace volume. A later Pod restart
# retains edits, deleted files, and the last published JAR.
if [[ ! -f "$source_dir/.training-initialized" ]]; then
  cp -an "$source_seed/." "$source_dir/"
  touch "$source_dir/.training-initialized"
fi
if [[ ! -f "$runtime_dir/.training-initialized" ]]; then
  cp -an "$runtime_seed/." "$runtime_dir/"
  touch "$runtime_dir/.training-initialized"
fi
stop_child() {
  if [[ -n "$child_pid" ]]; then
    kill -TERM "$child_pid" 2>/dev/null || true
    for ((tick=0; tick<stop_timeout*5; tick++)); do
      kill -0 "$child_pid" 2>/dev/null || break
      sleep 0.2
    done
    if kill -0 "$child_pid" 2>/dev/null; then kill -KILL "$child_pid" 2>/dev/null || true; fi
    wait "$child_pid" 2>/dev/null || true
    child_pid=
  fi
  rm -f "$state_dir/java.pid"
}
cleanup() {
  trap - EXIT TERM INT
  stop_child
  rm -f "$state_dir/supervisor.pid" "$state_dir/paused" "$state_dir/pause"
}
trap cleanup EXIT
trap 'exit 0' TERM INT
rm -f "$state_dir/java.pid" "$state_dir/paused" "$state_dir/pause"
printf '%s\n' "$$" > "$state_dir/supervisor.pid"

while :; do
  if [[ -f "$state_dir/pause" ]]; then
    stop_child
    touch "$state_dir/paused"
  else
    rm -f "$state_dir/paused"
    if [[ -n "$child_pid" ]] && ! kill -0 "$child_pid" 2>/dev/null; then
      wait "$child_pid" 2>/dev/null || true
      child_pid=
      rm -f "$state_dir/java.pid"
      # Avoid a tight restart loop when a source edit prevents Spring startup.
      sleep 1
    fi
    if [[ -z "$child_pid" && ! -f "$state_dir/pause" ]]; then
      (
        cd "$app_dir"
        exec java -jar "$runtime_dir/mall-portal.jar" \
          "--spring.config.additional-location=file:$app_dir/config/"
      ) &
      child_pid=$!
      printf '%s\n' "$child_pid" > "$state_dir/java.pid"
    fi
  fi
  sleep 0.2
done
