#!/usr/bin/env bash
# Run the real supervisor and rebuild helper; Java/Maven are external boundaries.
set -euo pipefail

test_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
entrypoint="$test_dir/../adaptation/portal-entrypoint.sh"
rebuild="$test_dir/../adaptation/portal-rebuild.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/portal-rebuild.XXXXXX")
supervisor_pid=
build_pid=
cleanup() {
  if [[ -n "$build_pid" ]]; then kill -TERM "$build_pid" 2>/dev/null || true; wait "$build_pid" 2>/dev/null || true; fi
  if [[ -n "$supervisor_pid" ]]; then kill -TERM "$supervisor_pid" 2>/dev/null || true; wait "$supervisor_pid" 2>/dev/null || true; fi
  case "$test_root" in "${TMPDIR:-/tmp}"/portal-rebuild.*) rm -rf -- "$test_root";; esac
}
trap cleanup EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_equal() { [[ "$1" == "$2" ]] || fail "$3: expected [$2], got [$1]"; }
await_file() {
  for ((attempt=0; attempt<150; attempt++)); do [[ -s "$1" ]] && return 0; sleep 0.1; done
  [[ ! -f "$test_root/supervisor.log" ]] || cat "$test_root/supervisor.log" >&2
  fail "timed out waiting for $1"
}
await_starts() {
  for ((attempt=0; attempt<150; attempt++)); do
    if [[ -f "$JAVA_EVENTS" ]] && [[ "$(wc -l < "$JAVA_EVENTS")" -ge "$1" ]]; then return 0; fi
    sleep 0.1
  done
  cat "$test_root/supervisor.log" >&2
  fail 'application did not start'
}

mkdir -p "$test_root/bin" "$test_root/seed/source" "$test_root/seed/runtime/config" "$test_root/cache"
printf '<project/>\n' > "$test_root/seed/source/pom.xml"
printf 'original source\n' > "$test_root/seed/source/student.java"
printf 'original jar\n' > "$test_root/seed/runtime/mall-portal.jar"
printf 'original config\n' > "$test_root/seed/runtime/config/application-sandbox.yml"
cat > "$test_root/bin/java" <<'FAKE_JAVA'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -jar && "$2" == "$TRAINING_RUNTIME_DIR/mall-portal.jar" ]] || exit 91
[[ "$3" == "--spring.config.additional-location=file:$TRAINING_APP_DIR/config/" ]] || exit 92
[[ "$PWD" == "$TRAINING_APP_DIR" ]] || exit 93
printf '%s\n' "$(cat "$2")" >> "$JAVA_EVENTS"
trap 'exit 0' TERM INT
while :; do sleep 0.1; done
FAKE_JAVA
cat > "$test_root/bin/mvn" <<'FAKE_MAVEN'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "-o -B -Dmaven.repo.local=$TRAINING_MAVEN_REPOSITORY -pl mall-portal -am package -DskipTests -Ddocker.skip=true" ]] || exit 94
[[ "$MAVEN_OPTS" == *-Xmx384m* ]] || exit 95
[[ ! -f "$TRAINING_STATE_DIR/java.pid" ]] || exit 96
[[ "$PWD" == "$TRAINING_SOURCE_DIR" ]] || exit 97
[[ ! -e mall-portal/target/classes/removed.yml && ! -e mall-common/target/classes/removed.class ]] || exit 98
if [[ -n "${BUILD_HOLD:-}" ]]; then
  printf 'building\n' > "$BUILD_HOLD.started"
  while [[ -f "$BUILD_HOLD" ]]; do sleep 0.1; done
fi
mkdir -p mall-portal/target
cat student.java > mall-portal/target/mall-portal-1.0-SNAPSHOT.jar
if [[ "${BUILD_FAIL:-0}" == 1 ]]; then exit 7; fi
if [[ "${BUILD_NO_JAR:-0}" == 1 ]]; then rm mall-portal/target/mall-portal-1.0-SNAPSHOT.jar; fi
FAKE_MAVEN
chmod +x "$test_root/bin/java" "$test_root/bin/mvn"
export PATH="$test_root/bin:$PATH"
export TRAINING_SOURCE_SEED="$test_root/seed/source"
export TRAINING_RUNTIME_SEED="$test_root/seed/runtime"
export TRAINING_MAVEN_REPOSITORY="$test_root/cache"
export JAVA_EVENTS="$test_root/java-events"
export JWT_SECRET='do-not-log-this-test-secret'
export TRAINING_STOP_TIMEOUT=2

select_workspace() {
  export TRAINING_SOURCE_DIR="$test_root/$1/mall"
  export TRAINING_RUNTIME_DIR="$test_root/$1/runtime"
  export TRAINING_APP_DIR="$test_root/$1/app"
  export TRAINING_STATE_DIR="$test_root/$1/state"
  mkdir -p "$TRAINING_APP_DIR"
}
start_supervisor() {
  bash "$entrypoint" > "$test_root/supervisor.log" 2>&1 &
  supervisor_pid=$!
  await_file "$TRAINING_STATE_DIR/java.pid"
}
stop_supervisor() {
  local child_pid
  child_pid=$(cat "$TRAINING_STATE_DIR/java.pid")
  kill -TERM "$supervisor_pid"
  wait "$supervisor_pid" || fail 'supervisor must shut down gracefully'
  supervisor_pid=
  ! kill -0 "$child_pid" 2>/dev/null || fail 'shutdown left a Java child running'
}
run_rebuild() { bash "$rebuild" > "$test_root/rebuild.log" 2>&1; }

# This startup/publish contract must fail before the helpers exist.
select_workspace workspace
start_supervisor
await_starts 1
assert_equal "$(cat "$TRAINING_SOURCE_DIR/student.java")" 'original source' 'initial source'
assert_equal "$(cat "$TRAINING_RUNTIME_DIR/config/application-sandbox.yml")" 'original config' 'initial config'
printf 'edited source\n' > "$TRAINING_SOURCE_DIR/student.java"
printf 'edited config\n' > "$TRAINING_RUNTIME_DIR/config/application-sandbox.yml"
# Deleted/renamed source resources must not remain in incremental build output.
mkdir -p "$TRAINING_SOURCE_DIR/mall-portal/target/classes" "$TRAINING_SOURCE_DIR/mall-common/target/classes"
printf 'deleted resource\n' > "$TRAINING_SOURCE_DIR/mall-portal/target/classes/removed.yml"
printf 'deleted class\n' > "$TRAINING_SOURCE_DIR/mall-common/target/classes/removed.class"
run_rebuild || { cat "$test_root/rebuild.log" >&2; fail 'successful build must publish and restart'; }
await_starts 2
assert_equal "$(tail -n 1 "$JAVA_EVENTS")" 'edited source' 'JVM runs rebuilt artifact'
assert_equal "$(cat "$TRAINING_RUNTIME_DIR/mall-portal.jar")" 'edited source' 'published runtime JAR'

export BUILD_FAIL=1
printf 'broken source\n' > "$TRAINING_SOURCE_DIR/student.java"
if run_rebuild; then fail 'compile failure must return nonzero'; fi
unset BUILD_FAIL
await_starts 3
assert_equal "$(cat "$TRAINING_RUNTIME_DIR/mall-portal.jar")" 'edited source' 'compile failure retains last good JAR'
assert_equal "$(tail -n 1 "$JAVA_EVENTS")" 'edited source' 'compile failure restarts last good JAR'
assert_equal "$(cat "$TRAINING_SOURCE_DIR/student.java")" 'broken source' 'failed build retains source edits'

export BUILD_NO_JAR=1
if run_rebuild; then fail 'missing build output must fail'; fi
unset BUILD_NO_JAR
await_starts 4
assert_equal "$(tail -n 1 "$JAVA_EVENTS")" 'edited source' 'missing output retains last good JAR'

export BUILD_HOLD="$test_root/build-hold"
: > "$BUILD_HOLD"
bash "$rebuild" > "$test_root/first-rebuild.log" 2>&1 &
build_pid=$!
await_file "$BUILD_HOLD.started"
if run_rebuild; then fail 'concurrent rebuild must fail'; fi
[[ "$(cat "$test_root/rebuild.log")" == *'already running'* ]] || fail 'concurrent build needs an actionable error'
rm "$BUILD_HOLD"
wait "$build_pid" || { cat "$test_root/first-rebuild.log" >&2; fail 'first concurrent rebuild failed'; }
build_pid=
unset BUILD_HOLD
await_starts 5
assert_equal "$(tail -n 1 "$JAVA_EVENTS")" 'broken source' 'lock owner publishes its result'

export BUILD_HOLD="$test_root/interrupted-build"
: > "$BUILD_HOLD"
printf 'interrupted source\n' > "$TRAINING_SOURCE_DIR/student.java"
bash "$rebuild" > "$test_root/interrupted-rebuild.log" 2>&1 &
build_pid=$!
await_file "$BUILD_HOLD.started"
kill -TERM "$build_pid"
if wait "$build_pid"; then fail 'interrupted rebuild must return nonzero'; fi
build_pid=
unset BUILD_HOLD
await_starts 6
assert_equal "$(tail -n 1 "$JAVA_EVENTS")" 'broken source' 'interrupted build restarts last good JAR'
[[ ! -d "$TRAINING_STATE_DIR/rebuild.lock" ]] || fail 'interrupted build left its lock held'

# Source mistakes must not prevent the last published application from running.
rm "$TRAINING_SOURCE_DIR/pom.xml"
stop_supervisor
start_supervisor
await_starts 7
[[ ! -e "$TRAINING_SOURCE_DIR/pom.xml" ]] || fail 'restart silently restored a deleted source file'
if run_rebuild; then fail 'a source tree without its POM must not rebuild'; fi
[[ "$(cat "$test_root/rebuild.log")" == *'pom.xml'* ]] || fail 'missing POM needs a useful source error'
assert_equal "$(cat "$TRAINING_SOURCE_DIR/student.java")" 'interrupted source' 'restart preserves source'
assert_equal "$(cat "$TRAINING_RUNTIME_DIR/config/application-sandbox.yml")" 'edited config' 'restart preserves config'
assert_equal "$(tail -n 1 "$JAVA_EVENTS")" 'broken source' 'restart preserves rebuilt JAR'
stop_supervisor

select_workspace fresh-workspace
start_supervisor
await_starts 8
assert_equal "$(cat "$TRAINING_SOURCE_DIR/student.java")" 'original source' 'fresh workspace resets source'
assert_equal "$(tail -n 1 "$JAVA_EVENTS")" 'original jar' 'fresh workspace resets deployed JAR'
stop_supervisor

if cat "$test_root"/*.log | grep -q "$JWT_SECRET"; then fail 'scripts logged a secret'; fi
bash -n "$entrypoint" "$rebuild"
printf 'PASS: portal publication, rollback, interruption, locking, persistent workspace and graceful shutdown\n'
