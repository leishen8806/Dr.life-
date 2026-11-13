#!/bin/sh
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH
test "$#" = 1
app=$1
case "$app" in buyer|merchant|admin) ;; *) exit 64;; esac
site_root=${STAGING_SITE_ROOT:-/srv/drlifeplus-staging}/$app
current="$site_root/current"; releases="$site_root/releases"
test -L "$current"
current_target=$(readlink -f "$current"); previous=''
for candidate in $(find "$releases" -mindepth 1 -maxdepth 1 -type d -print | sort); do
  test "$(readlink -f "$candidate")" = "$current_target" && continue
  previous=$candidate
done
test -n "$previous"
ln -sfn "releases/$(basename "$previous")" "$site_root/.current.next"
rm -f "$current"
mv "$site_root/.current.next" "$current"
printf 'rolled back to %s\n' "$(readlink "$current")"
