#!/bin/sh
# Recompile the editable source with the image's installed dependencies only.
set -eu

fail() { printf 'training-rebuild: %s\n' "$*" >&2; exit 1; }

source_dir=${TRAINING_SOURCE_DIR:-/workspace/mall-app-web}
web_root=${TRAINING_WEB_ROOT:-/usr/share/nginx/html}
releases_dir=${TRAINING_RELEASES_DIR:-/workspace/runtime/releases}
current_link=${TRAINING_CURRENT_LINK:-/workspace/runtime/current}

[ -d "$source_dir/node_modules" ] || fail 'Installed dependencies are missing; rebuild the training image.'
source_dir=$(CDPATH= cd -- "$source_dir" && pwd -P)
mkdir -p "$releases_dir"
releases_dir=$(CDPATH= cd -- "$releases_dir" && pwd -P)
current_parent=$(CDPATH= cd -- "$(dirname -- "$current_link")" && pwd -P)
current_link="$current_parent/$(basename -- "$current_link")"

lock_dir="$releases_dir/.training-build.lock"
if ! mkdir "$lock_dir" 2>/dev/null; then
  fail 'A frontend build is already running; wait for it to finish.'
fi
staged_release=
previous_release=
next_link="$current_link.next.$$"
cleanup() {
  cleanup_status=$?
  trap - EXIT
  trap '' HUP INT TERM
  # A signal can arrive between the pointer swap and the successful reload.
  if [ -n "$staged_release" ] && [ "$(readlink "$current_link")" = "$staged_release" ]; then
    rm -f "$next_link"
    ln -s "$previous_release" "$next_link"
    mv -Tf "$next_link" "$current_link"
    nginx -s reload >/dev/null 2>&1 || true
  fi
  rm -f "$next_link"
  if [ -n "$staged_release" ]; then rm -rf "$staged_release"; fi
  rmdir "$lock_dir"
  exit "$cleanup_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM

# Capture the rollback/retention target only after acquiring the publication lock.
[ -L "$current_link" ] || fail 'The live HTML directory must be the managed release symlink.'
previous_release=$(CDPATH= cd -- "$current_link" && pwd -P)
case "$previous_release" in
  "$releases_dir"/release.*) ;;
  *) fail 'The live HTML directory points outside the managed releases.' ;;
esac

export npm_config_offline=true npm_config_audit=false npm_config_fund=false
export VITE_API_BASE_URL=api VITE_USE_ALIPAY=false
export NODE_OPTIONS="${NODE_OPTIONS:---max-old-space-size=640}"
cd -- "$source_dir"
printf 'Building frontend from %s using installed offline dependencies...\n' "$source_dir"
output_dir="$source_dir/dist/build/h5"
# Build output is separate from the live releases; require this run to create it.
rm -rf "$output_dir"
if ! npm --offline --no-audit --no-fund run build:h5; then
  fail 'Compilation failed; the previous site is still running.'
fi
[ -s "$output_dir/index.html" ] || fail 'Compilation did not produce index.html; the previous site is still running.'

staged_release=$(mktemp -d "$releases_dir/release.XXXXXX")
cp -a "$output_dir/." "$staged_release/"
if ! nginx -t; then
  fail 'Nginx configuration is invalid; the previous site is still running.'
fi
ln -s "$staged_release" "$next_link"
# -T replaces the link itself instead of moving it into its target directory.
mv -Tf "$next_link" "$current_link"
if ! nginx -s reload; then
  ln -s "$previous_release" "$next_link"
  mv -Tf "$next_link" "$current_link"
  nginx -s reload >/dev/null 2>&1 || true
  fail 'Nginx reload failed; the previous site has been restored.'
fi

active_release=$staged_release
staged_release=
# Retain current and previous releases while bounding disk usage.
for release in "$releases_dir"/release.*; do
  [ "$release" = "$active_release" ] && continue
  [ "$release" = "$previous_release" ] && continue
  [ -e "$release" ] || [ -L "$release" ] || continue
  rm -rf "$release"
done
printf 'Frontend rebuilt and published at %s. Source edits are preserved.\n' "$web_root"
