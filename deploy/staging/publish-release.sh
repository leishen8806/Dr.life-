#!/bin/sh
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH
test "$#" = 2
app=$1; release_id=$2
case "$app" in buyer|merchant|admin) ;; *) exit 64;; esac
case "$release_id" in ''|*[!A-Za-z0-9-]*) exit 64;; esac
site_root=${STAGING_SITE_ROOT:-/srv/drlifeplus-staging}/$app
incoming="$site_root/incoming/$release_id"
releases="$site_root/releases"
test -d "$incoming"; test -d "$releases"; test ! -e "$releases/$release_id"
mv "$incoming" "$releases/$release_id"
ln -sfn "releases/$release_id" "$site_root/.current.next"
if ! mv -Tf "$site_root/.current.next" "$site_root/current" 2>/dev/null; then
  # BSD mv lacks -T; ln -hfs still replaces the symlink target without a
  # window where current is absent, and the Linux path above stays atomic.
  ln -hfs "$(readlink "$site_root/.current.next")" "$site_root/current"
  rm -f "$site_root/.current.next"
fi
current_target=$(readlink -f "$site_root/current")
kept=0
for release_dir in $(find "$releases" -mindepth 1 -maxdepth 1 -type d -print | sort -r); do
  test -d "$release_dir" || continue
  test "$(readlink -f "$release_dir")" = "$current_target" && continue
  if test "$kept" -lt 4; then
    kept=$((kept + 1))
  else
    rm -rf -- "$release_dir"
  fi
done
