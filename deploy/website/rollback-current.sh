#!/bin/sh
set -eu

site_root=/srv/settlecore
current="$site_root/current"
releases="$site_root/releases"

if ! test -L "$current"; then
  printf '%s\n' 'current release link is missing' >&2
  exit 1
fi
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

if test "$found_current" != true; then
  printf '%s\n' 'current release is not in releases' >&2
  exit 1
fi
if test -z "$previous"; then
  printf '%s\n' 'no earlier release is available' >&2
  exit 1
fi
ln -s "releases/$(basename "$previous")" "$site_root/.current.next"
mv -Tf "$site_root/.current.next" "$current"
printf 'rolled back to %s\n' "$(readlink "$current")"
