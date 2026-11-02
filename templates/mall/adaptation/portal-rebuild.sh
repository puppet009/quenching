#!/usr/bin/env bash
# Rebuild offline and restart only the child JVM, preserving the container/PVC.
set -euo pipefail

source_dir=${TRAINING_SOURCE_DIR:-/workspace/mall}
runtime_dir=${TRAINING_RUNTIME_DIR:-/workspace/runtime}
state_dir=${TRAINING_STATE_DIR:-/run/training}
repository=${TRAINING_MAVEN_REPOSITORY:-/opt/training/m2}
stop_timeout=${TRAINING_STOP_TIMEOUT:-10}
lock_dir="$state_dir/rebuild.lock"
candidate=
build_pid=
paused=0

if [[ ! -f "$source_dir/pom.xml" ]]; then
  printf 'No pom.xml exists at %s. Restore it before rebuilding.\n' "$source_dir" >&2
  exit 1
fi
if [[ ! -f "$state_dir/supervisor.pid" ]] ||
   ! kill -0 "$(cat "$state_dir/supervisor.pid" 2>/dev/null)" 2>/dev/null; then
  printf 'The training application supervisor is not running.\n' >&2
  exit 1
fi
if ! mkdir "$lock_dir" 2>/dev/null; then
  printf 'A training rebuild is already running. Wait for it to finish.\n' >&2
  exit 1
fi

finish() {
  local result=$?
  trap - EXIT TERM INT
  if [[ -n "$build_pid" ]]; then
    kill -TERM "$build_pid" 2>/dev/null || true
    wait "$build_pid" 2>/dev/null || true
  fi
  [[ -z "$candidate" ]] || rm -f "$candidate"
  if [[ "$paused" == 1 ]]; then
    rm -f "$state_dir/pause"
    local resumed=0
    for ((tick=0; tick<(stop_timeout+10)*5; tick++)); do
      if [[ ! -f "$state_dir/paused" && -s "$state_dir/java.pid" ]] &&
         kill -0 "$(cat "$state_dir/java.pid" 2>/dev/null)" 2>/dev/null; then
        resumed=1
        break
      fi
      sleep 0.2
    done
    if [[ "$resumed" == 0 ]]; then
      printf 'The application did not restart; check the container logs.\n' >&2
      result=1
    elif [[ "$result" == 0 ]]; then
      printf 'Build published. The application is starting; check its health before continuing.\n'
    else
      printf 'Build failed. The last published JAR is starting again; source edits are preserved.\n' >&2
    fi
  fi
  rmdir "$lock_dir" 2>/dev/null || true
  exit "$result"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

paused=1
touch "$state_dir/pause"
for ((tick=0; tick<(stop_timeout+10)*5; tick++)); do
  [[ ! -f "$state_dir/paused" ]] || break
  sleep 0.2
done
if [[ ! -f "$state_dir/paused" ]]; then
  printf 'The application did not stop for the rebuild.\n' >&2
  exit 1
fi

printf 'Building mall-portal offline; the application is temporarily stopped.\n'
cd "$source_dir"
# A package build alone leaves deleted resources/classes in target. Remove
# generated output without fetching Maven's otherwise unused clean plugin.
find "$source_dir" -type d -name target -prune -exec rm -rf '{}' +
export MAVEN_OPTS=${TRAINING_MAVEN_OPTS:--Xmx384m -XX:MaxMetaspaceSize=192m}
mvn -o -B "-Dmaven.repo.local=$repository" -pl mall-portal -am package \
  -DskipTests -Ddocker.skip=true &
build_pid=$!
if wait "$build_pid"; then
  build_pid=
else
  result=$?
  build_pid=
  exit "$result"
fi

built_jar="$source_dir/mall-portal/target/mall-portal-1.0-SNAPSHOT.jar"
if [[ ! -s "$built_jar" ]]; then
  printf 'Maven did not produce the mall-portal JAR.\n' >&2
  exit 1
fi
candidate=$(mktemp "$runtime_dir/.mall-portal.jar.XXXXXX")
cp "$built_jar" "$candidate"
mv -f "$candidate" "$runtime_dir/mall-portal.jar"
candidate=
