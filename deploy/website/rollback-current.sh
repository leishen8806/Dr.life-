#!/usr/bin/env sh
set -eu

site_root=/srv/settlecore
current="$site_root/current"
releases="$site_root/releases"

test -L "$current"
current_target=$(readlink -f "$current")
previous=''
found_current=false

for candidate in $(find "$releases" -mindepth 1 -maxdepth 1 -type d -print | sort); do
  if test "$(readlink -f "$candidate")" = "$current_target"; then
    found_current=true
    break
  fi
  previous=$candidate
done

test "$found_current" = true
test -n "$previous"
ln -s "releases/$(basename "$previous")" "$site_root/.current.next"
mv -Tf "$site_root/.current.next" "$current"
printf 'rolled back to %s\n' "$(readlink "$current")"
