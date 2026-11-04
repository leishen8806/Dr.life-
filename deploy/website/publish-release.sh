#!/usr/bin/env sh
set -eu

test "$#" = 1
release_id=$1
case "$release_id" in
  ''|*[!A-Za-z0-9-]*) exit 64 ;;
esac

site_root=/srv/settlecore
incoming="$site_root/incoming/$release_id"
releases="$site_root/releases"
test -d "$incoming"
test ! -e "$releases/$release_id"

mv "$incoming" "$releases/$release_id"
ln -s "releases/$release_id" "$site_root/.current.next"
mv -Tf "$site_root/.current.next" "$site_root/current"
ls -1dt "$releases"/* | tail -n +6 | xargs -r rm -rf
