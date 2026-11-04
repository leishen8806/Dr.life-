#!/usr/bin/env sh
set -eu

site_root=${SITE_ROOT:-/srv/settlecore}
current="$site_root/current"
releases="$site_root/releases"

test -L "$current"
current_target=$(readlink -f "$current")
previous=$(find "$releases" -mindepth 1 -maxdepth 1 -type d -print | sort -r | while IFS= read -r candidate; do
  test "$(readlink -f "$candidate")" = "$current_target" || { printf '%s\n' "$candidate"; break; }
done)

test -n "$previous"
ln -s "$previous" "$site_root/.current.next"
mv -Tf "$site_root/.current.next" "$current"
printf 'rolled back to %s\n' "$(readlink "$current")"
