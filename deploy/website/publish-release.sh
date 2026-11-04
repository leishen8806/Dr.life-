#!/usr/bin/env sh
set -eu

test "$#" = 1
release_id=$1
case "$release_id" in
  ''|*[!A-Za-z0-9-]*) exit 64 ;;
esac

site_root=/srv/settlecore
incoming="$site_root/incoming/$release_id"
staging="$site_root/staging"
staged="$staging/$release_id"
new="$staging/.new-$release_id"
releases="$site_root/releases"
test -d "$incoming"
test -d "$staging"
test ! -e "$staged"
test ! -e "$new"
test ! -e "$releases/$release_id"

mv "$incoming" "$staged"

# The deploy account cannot enter staging, so validation and copying happen
# after its upload has been removed from its writable tree.
if find "$staged" ! -type d ! -type f -print -quit | grep -q .; then
  printf '%s\n' 'release contains a symbolic link or special file' >&2
  rm -rf "$staged"
  exit 1
fi

mkdir -m 0700 "$new"
if ! cp -R "$staged/." "$new/"; then
  rm -rf "$new"
  exit 1
fi

chown -R root:root "$new"
find "$new" -type d -exec chmod 0755 {} +
find "$new" -type f -exec chmod 0644 {} +
mv "$new" "$releases/$release_id"
rm -rf "$staged"
ln -s "releases/$release_id" "$site_root/.current.next"
mv -Tf "$site_root/.current.next" "$site_root/current"
ls -1dt "$releases"/* | tail -n +6 | xargs -r rm -rf
