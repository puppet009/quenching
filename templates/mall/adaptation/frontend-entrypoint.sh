#!/bin/sh
# Seed each class PVC once, then preserve its source, releases and nginx config.
set -eu

workspace_dir=${TRAINING_WORKSPACE_DIR:-/workspace}
source_seed=${TRAINING_SOURCE_SEED_DIR:-/opt/training/frontend-source}
dependencies_dir=${TRAINING_NODE_MODULES_DIR:-/opt/training/frontend-node_modules}
html_seed=${TRAINING_HTML_SEED_DIR:-/opt/training/frontend-html}
config_seed=${TRAINING_NGINX_CONFIG_SEED:-/opt/training/nginx.conf}
web_root=${TRAINING_WEB_ROOT:-/usr/share/nginx/html}
config_link=${TRAINING_NGINX_CONFIG_LINK:-/etc/nginx/conf.d/default.conf}
stock_entrypoint=${TRAINING_NGINX_ENTRYPOINT:-/docker-entrypoint.sh}

mkdir -p "$workspace_dir"
workspace_dir=$(CDPATH= cd -- "$workspace_dir" && pwd -P)
source_dir="$workspace_dir/mall-app-web"
runtime_dir="$workspace_dir/runtime"
releases_dir="$runtime_dir/releases"
current_link="$runtime_dir/current"
mkdir -p "$releases_dir" "$(dirname -- "$web_root")" "$(dirname -- "$config_link")"
seed_temp=
trap 'if [ -n "$seed_temp" ]; then rm -rf "$seed_temp"; fi' EXIT

if [ ! -d "$source_dir" ]; then
  seed_temp=$(mktemp -d "$workspace_dir/.frontend-source.XXXXXX")
  cp -a "$source_seed/." "$seed_temp/"
  ln -s "$dependencies_dir" "$seed_temp/node_modules"
  mv "$seed_temp" "$source_dir"
  seed_temp=
elif [ ! -e "$source_dir/node_modules" ] && [ ! -L "$source_dir/node_modules" ]; then
  ln -s "$dependencies_dir" "$source_dir/node_modules"
fi

if [ ! -L "$current_link" ]; then
  [ ! -e "$current_link" ] || { echo 'training-entrypoint: current release must be a symlink.' >&2; exit 1; }
  if [ ! -d "$releases_dir/release.initial" ]; then
    seed_temp=$(mktemp -d "$releases_dir/.frontend-html.XXXXXX")
    cp -a "$html_seed/." "$seed_temp/"
    mv "$seed_temp" "$releases_dir/release.initial"
    seed_temp=
  fi
  ln -s "$releases_dir/release.initial" "$current_link"
fi
[ -s "$current_link/index.html" ] || { echo 'training-entrypoint: the persisted HTML release is missing.' >&2; exit 1; }
if [ ! -e "$runtime_dir/nginx.conf" ]; then cp "$config_seed" "$runtime_dir/nginx.conf"; fi

# The image already has these links; creating missing links also supports fixtures.
if [ ! -L "$web_root" ]; then ln -s "$current_link" "$web_root"; fi
if [ ! -L "$config_link" ]; then ln -s "$runtime_dir/nginx.conf" "$config_link"; fi
# No build process from the previous container can still own this PVC lock.
rm -rf "$releases_dir/.training-build.lock"
exec "$stock_entrypoint" "$@"
