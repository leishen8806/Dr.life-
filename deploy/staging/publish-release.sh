#!/bin/sh
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH
test "$#" = 1
release_id=$1
case "$release_id" in ''|*[!A-Za-z0-9-]*) exit 64;; esac
site_root=${STAGING_SITE_ROOT:-/srv/drlifeplus-staging}
incoming="$site_root/incoming/$release_id"
releases="$site_root/releases"
test -d "$incoming"; test -d "$releases"; test ! -e "$releases/$release_id"
mv "$incoming" "$releases/$release_id"
ln -s "releases/$release_id" "$site_root/.current.next"
mv -Tf "$site_root/.current.next" "$site_root/current"
ls -1dt "$releases"/* | tail -n +6 | xargs -r rm -rf
