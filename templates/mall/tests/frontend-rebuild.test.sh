#!/bin/sh
# Exercise the real publisher; npm compilation and nginx are external boundaries.
set -eu

test_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
helper="$test_dir/../adaptation/frontend-rebuild.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/frontend rebuild.XXXXXX")
trap 'rm -rf "$test_root"' EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_equal() { [ "$1" = "$2" ] || fail "$3: expected [$2], got [$1]"; }

mkdir -p "$test_root/bin" "$test_root/source tree/node_modules" "$test_root/www/releases/release.initial"
printf 'original live page\n' > "$test_root/www/releases/release.initial/index.html"
printf 'student source edit\n' > "$test_root/source tree/student.vue"
ln -s "$test_root/www/releases/release.initial" "$test_root/www/html"

cat > "$test_root/bin/npm" <<'FAKE_NPM'
#!/bin/sh
set -eu
[ "$*" = '--offline --no-audit --no-fund run build:h5' ] || exit 91
[ "${npm_config_offline:-}" = true ] || exit 92
[ "${VITE_API_BASE_URL:-}" = api ] || exit 93
[ "${VITE_USE_ALIPAY:-}" = false ] || exit 94
if [ "${BUILD_SKIP_OUTPUT:-0}" = 1 ]; then exit 0; fi
mkdir -p dist/build/h5
printf '%s\n' "${BUILD_CONTENT:-new live page}" > dist/build/h5/index.html
if [ "${BUILD_FAIL:-0}" = 1 ]; then exit 7; fi
if [ "${BUILD_NO_INDEX:-0}" = 1 ]; then rm dist/build/h5/index.html; fi
FAKE_NPM

cat > "$test_root/bin/nginx" <<'FAKE_NGINX'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$NGINX_CALLS"
if [ "$*" = '-t' ] && [ "${NGINX_TEST_FAIL:-0}" = 1 ]; then exit 8; fi
if [ "$*" = '-s reload' ] && [ "${NGINX_SIGNAL_RELOAD:-0}" = 1 ]; then
  kill -TERM "$PPID"
  exit 10
fi
if [ "$*" = '-s reload' ] && [ "${NGINX_RELOAD_FAIL:-0}" = 1 ] && [ ! -f "$NGINX_CALLS.failed" ]; then
  : > "$NGINX_CALLS.failed"
  exit 9
fi
FAKE_NGINX
chmod +x "$test_root/bin/npm" "$test_root/bin/nginx"
export PATH="$test_root/bin:$PATH"
export TRAINING_SOURCE_DIR="$test_root/source tree"
export TRAINING_WEB_ROOT="$test_root/www/html"
export TRAINING_CURRENT_LINK="$test_root/www/html"
export TRAINING_RELEASES_DIR="$test_root/www/releases"
export NGINX_CALLS="$test_root/nginx calls"

# A missing publisher must fail this success contract, before implementation.
if ! sh "$helper" > "$test_root/output" 2>&1; then
  cat "$test_root/output" >&2
  fail 'successful compilation must publish and reload'
fi
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'new live page' 'published page'
assert_equal "$(cat "$TRAINING_SOURCE_DIR/student.vue")" 'student source edit' 'source is preserved'
assert_equal "$(cat "$NGINX_CALLS")" "$(printf '%s\n' '-t' '-s reload')" 'nginx validation and reload'
assert_equal "$(find "$TRAINING_RELEASES_DIR" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" 2 'keep active and previous releases'
active_release=$(readlink "$TRAINING_WEB_ROOT")

export BUILD_SKIP_OUTPUT=1
if sh "$helper" > "$test_root/output" 2>&1; then fail 'stale build output must not count as a fresh compilation'; fi
assert_equal "$(readlink "$TRAINING_WEB_ROOT")" "$active_release" 'stale output preserves active release'
unset BUILD_SKIP_OUTPUT

export BUILD_FAIL=1
: > "$NGINX_CALLS"
if sh "$helper" > "$test_root/output" 2>&1; then fail 'compile error must return failure'; fi
assert_equal "$(readlink "$TRAINING_WEB_ROOT")" "$active_release" 'compile error preserves active release'
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'new live page' 'compile error preserves live HTML'
assert_equal "$(cat "$NGINX_CALLS")" '' 'compile error does not reload nginx'
unset BUILD_FAIL

export BUILD_NO_INDEX=1
if sh "$helper" > "$test_root/output" 2>&1; then fail 'incomplete output must return failure'; fi
assert_equal "$(readlink "$TRAINING_WEB_ROOT")" "$active_release" 'incomplete output preserves active release'
unset BUILD_NO_INDEX

export NGINX_TEST_FAIL=1
if sh "$helper" > "$test_root/output" 2>&1; then fail 'invalid nginx config must return failure'; fi
assert_equal "$(readlink "$TRAINING_WEB_ROOT")" "$active_release" 'config error preserves active release'
unset NGINX_TEST_FAIL

export NGINX_RELOAD_FAIL=1
if sh "$helper" > "$test_root/output" 2>&1; then fail 'reload error must return failure'; fi
assert_equal "$(readlink "$TRAINING_WEB_ROOT")" "$active_release" 'reload error rolls back active release'
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'new live page' 'reload error restores live HTML'
unset NGINX_RELOAD_FAIL

export NGINX_SIGNAL_RELOAD=1
if sh "$helper" > "$test_root/output" 2>&1; then fail 'interrupted publication must return failure'; fi
assert_equal "$(readlink "$TRAINING_WEB_ROOT")" "$active_release" 'interrupted publication restores active release'
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'new live page' 'interrupted publication preserves live HTML'
unset NGINX_SIGNAL_RELOAD

mkdir "$TRAINING_RELEASES_DIR/.training-build.lock"
if sh "$helper" > "$test_root/output" 2>&1; then fail 'concurrent build must be rejected'; fi
case "$(cat "$test_root/output")" in *'already running'*) ;; *) fail 'concurrent build needs a friendly error';; esac
rmdir "$TRAINING_RELEASES_DIR/.training-build.lock"

export BUILD_CONTENT='latest live page'
sh "$helper" > "$test_root/output" 2>&1 || { cat "$test_root/output" >&2; fail 'build after failures must succeed'; }
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'latest live page' 'subsequent page'
assert_equal "$(find "$TRAINING_RELEASES_DIR" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" 2 'old releases are bounded'
assert_equal "$(cat "$TRAINING_SOURCE_DIR/student.vue")" 'student source edit' 'source remains after rebuilds'

# Reproduce another build completing immediately before this build gets its lock.
# Capturing the old release before mkdir would prune/restore the wrong release.
export REAL_MKDIR=$(command -v mkdir)
cat > "$test_root/bin/mkdir" <<'FAKE_MKDIR'
#!/bin/sh
set -eu
if [ "$#" = 1 ] && [ "$1" = "$TRAINING_RELEASES_DIR/.training-build.lock" ] && [ -n "${LOCK_HANDOFF_RELEASE:-}" ]; then
  ln -s "$LOCK_HANDOFF_RELEASE" "$TRAINING_CURRENT_LINK.handoff"
  mv -Tf "$TRAINING_CURRENT_LINK.handoff" "$TRAINING_CURRENT_LINK"
fi
exec "$REAL_MKDIR" "$@"
FAKE_MKDIR
chmod +x "$test_root/bin/mkdir"
export LOCK_HANDOFF_RELEASE="$TRAINING_RELEASES_DIR/release.concurrent-success"
"$REAL_MKDIR" "$LOCK_HANDOFF_RELEASE"
printf 'concurrently published site\n' > "$LOCK_HANDOFF_RELEASE/index.html"
sh "$helper" > "$test_root/output" 2>&1 || { cat "$test_root/output" >&2; fail 'build after concurrent publication must succeed'; }
[ -f "$LOCK_HANDOFF_RELEASE/index.html" ] || fail 'successful build must retain the release that was live when its lock was acquired'

export LOCK_HANDOFF_RELEASE="$TRAINING_RELEASES_DIR/release.concurrent-rollback"
"$REAL_MKDIR" "$LOCK_HANDOFF_RELEASE"
printf 'concurrent rollback target\n' > "$LOCK_HANDOFF_RELEASE/index.html"
rm -f "$NGINX_CALLS.failed"
export NGINX_RELOAD_FAIL=1
if sh "$helper" > "$test_root/output" 2>&1; then fail 'reload failure after concurrent publication must fail'; fi
assert_equal "$(readlink "$TRAINING_CURRENT_LINK")" "$LOCK_HANDOFF_RELEASE" 'rollback restores the release captured under the lock'
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'concurrent rollback target' 'rollback preserves the latest live site'
unset LOCK_HANDOFF_RELEASE NGINX_RELOAD_FAIL

# Validation now happens while locked; its errors must release the owned lock.
"$REAL_MKDIR" "$test_root/outside releases"
ln -s "$test_root/outside releases" "$TRAINING_CURRENT_LINK.invalid"
mv -Tf "$TRAINING_CURRENT_LINK.invalid" "$TRAINING_CURRENT_LINK"
if sh "$helper" > "$test_root/output" 2>&1; then fail 'unmanaged current release must be rejected'; fi
[ ! -e "$TRAINING_RELEASES_DIR/.training-build.lock" ] || fail 'release validation failure must remove the owned build lock'
sh -n "$helper"
printf 'PASS: frontend rebuild publication, failure isolation, rollback, locking and retention\n'
