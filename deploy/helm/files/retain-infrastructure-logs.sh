#!/bin/sh
# Only dated, non-symlink directories for the collector's fixed service list.
set -eu
LOG_ROOT="${LOG_ROOT:-/var/log/quenching}"
case "$LOG_ROOT" in /*) ;; *) exit 2 ;; esac
[ "$LOG_ROOT" != / ] && [ -d "$LOG_ROOT" ] && [ ! -L "$LOG_ROOT" ] || exit 2
for service in web admin postgresql redis rabbitmq prometheus-server; do
  directory="$LOG_ROOT/$service"
  [ -d "$directory" ] && [ ! -L "$directory" ] || continue
  for day in "$directory"/????-??-??; do
    [ -d "$day" ] && [ ! -L "$day" ] || continue
    find "$day" -maxdepth 1 -type f -name "$service-*.log" -mtime +30 -exec rm -f -- {} +
    rmdir -- "$day" 2>/dev/null || true
  done
done
