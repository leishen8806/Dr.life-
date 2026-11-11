#!/bin/sh
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH
site_root=${STAGING_SITE_ROOT:-/srv/drlifeplus-staging}
current="$site_root/current"; releases="$site_root/releases"
test -L "$current"
current_target=$(readlink -f "$current"); previous=''
for candidate in $(find "$releases" -mindepth 1 -maxdepth 1 -type d -print | sort); do
  test "$(readlink -f "$candidate")" = "$current_target" && continue
  previous=$candidate
done
test -n "$previous"
ln -s "releases/$(basename "$previous")" "$site_root/.current.next"
mv -Tf "$site_root/.current.next" "$current"
printf 'rolled back to %s\n' "$(readlink "$current")"
