#!/bin/sh
set -eu

test_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
entrypoint="$test_dir/../adaptation/frontend-entrypoint.sh"
rebuild="$test_dir/../adaptation/frontend-rebuild.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/frontend startup.XXXXXX")
trap 'rm -rf "$test_root"' EXIT HUP INT TERM
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_equal() { [ "$1" = "$2" ] || fail "$3: expected [$2], got [$1]"; }

mkdir -p "$test_root/image/source/src" "$test_root/image/dependencies" "$test_root/image/html" "$test_root/bin"
printf 'baseline Vue source\n' > "$test_root/image/source/src/App.vue"
printf 'baseline site\n' > "$test_root/image/html/index.html"
printf 'baseline nginx config\n' > "$test_root/image/nginx.conf"
printf 'cached dependency\n' > "$test_root/image/dependencies/installed.txt"
cat > "$test_root/bin/stock-entrypoint" <<'FAKE_STOCK'
#!/bin/sh
printf '%s\n' "$@" > "$ENTRYPOINT_CALLS"
FAKE_STOCK
cat > "$test_root/bin/npm" <<'FAKE_NPM'
#!/bin/sh
mkdir -p dist/build/h5
printf 'rebuilt student site\n' > dist/build/h5/index.html
FAKE_NPM
cat > "$test_root/bin/nginx" <<'FAKE_NGINX'
#!/bin/sh
exit 0
FAKE_NGINX
chmod +x "$test_root/bin/stock-entrypoint" "$test_root/bin/npm" "$test_root/bin/nginx"
export PATH="$test_root/bin:$PATH"
export TRAINING_SOURCE_SEED_DIR="$test_root/image/source"
export TRAINING_NODE_MODULES_DIR="$test_root/image/dependencies"
export TRAINING_HTML_SEED_DIR="$test_root/image/html"
export TRAINING_NGINX_CONFIG_SEED="$test_root/image/nginx.conf"
export TRAINING_NGINX_ENTRYPOINT="$test_root/bin/stock-entrypoint"
export ENTRYPOINT_CALLS="$test_root/entrypoint calls"
export TRAINING_WEB_ROOT="$test_root/container/html"
export TRAINING_NGINX_CONFIG_LINK="$test_root/container/default.conf"
export TRAINING_WORKSPACE_DIR="$test_root/class one"

if ! sh "$entrypoint" nginx -g 'daemon off;' > "$test_root/output" 2>&1; then
  cat "$test_root/output" >&2
  fail 'fresh workspace must initialize before starting nginx'
fi
assert_equal "$(cat "$TRAINING_WORKSPACE_DIR/mall-app-web/src/App.vue")" 'baseline Vue source' 'source seed'
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'baseline site' 'HTML seed'
assert_equal "$(cat "$TRAINING_NGINX_CONFIG_LINK")" 'baseline nginx config' 'nginx config seed'
assert_equal "$(readlink "$TRAINING_WORKSPACE_DIR/mall-app-web/node_modules")" "$TRAINING_NODE_MODULES_DIR" 'dependency cache link'
assert_equal "$(cat "$ENTRYPOINT_CALLS")" "$(printf '%s\n' nginx -g 'daemon off;')" 'stock nginx entrypoint arguments'

printf 'student Vue edit\n' > "$TRAINING_WORKSPACE_DIR/mall-app-web/src/App.vue"
printf 'student nginx config\n' > "$TRAINING_NGINX_CONFIG_LINK"
export TRAINING_SOURCE_DIR="$TRAINING_WORKSPACE_DIR/mall-app-web"
export TRAINING_RELEASES_DIR="$TRAINING_WORKSPACE_DIR/runtime/releases"
export TRAINING_CURRENT_LINK="$TRAINING_WORKSPACE_DIR/runtime/current"
sh "$rebuild" > "$test_root/output" 2>&1 || { cat "$test_root/output" >&2; fail 'persistent source must rebuild'; }
mkdir "$TRAINING_RELEASES_DIR/.training-build.lock"
sh "$entrypoint" nginx -g 'daemon off;' > "$test_root/output" 2>&1 || { cat "$test_root/output" >&2; fail 'existing workspace must restart'; }
assert_equal "$(cat "$TRAINING_SOURCE_DIR/src/App.vue")" 'student Vue edit' 'restart preserves source'
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'rebuilt student site' 'restart preserves rebuilt HTML'
assert_equal "$(cat "$TRAINING_NGINX_CONFIG_LINK")" 'student nginx config' 'restart preserves nginx config'
[ ! -e "$TRAINING_RELEASES_DIR/.training-build.lock" ] || fail 'restart must remove the previous container build lock'

# A different class workspace must start with a clean baseline.
export TRAINING_WORKSPACE_DIR="$test_root/class two"
export TRAINING_WEB_ROOT="$test_root/new container/html"
export TRAINING_NGINX_CONFIG_LINK="$test_root/new container/default.conf"
unset TRAINING_SOURCE_DIR TRAINING_RELEASES_DIR TRAINING_CURRENT_LINK
sh "$entrypoint" nginx -g 'daemon off;' > "$test_root/output" 2>&1 || { cat "$test_root/output" >&2; fail 'new class must initialize'; }
assert_equal "$(cat "$TRAINING_WORKSPACE_DIR/mall-app-web/src/App.vue")" 'baseline Vue source' 'new class source resets'
assert_equal "$(cat "$TRAINING_WEB_ROOT/index.html")" 'baseline site' 'new class HTML resets'
assert_equal "$(cat "$TRAINING_NGINX_CONFIG_LINK")" 'baseline nginx config' 'new class nginx config resets'
sh -n "$entrypoint"
printf 'PASS: frontend persistent workspace startup and class reset\n'
