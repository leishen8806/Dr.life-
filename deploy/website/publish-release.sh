#!/bin/sh
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH

test "$#" = 1
release_id=$1
case "$release_id" in
  ''|*[!A-Za-z0-9-]*) exit 64 ;;
esac

site_root=/srv/drlifeplus
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
rm -rf "$site_root/.current.next"

validate_release_tree() {
  if find "$1" -type f -links +1 -print -quit | grep -q .; then
    printf '%s\n' 'release contains a multiply linked file' >&2
    return 1
  fi
  if find "$1" ! -type d ! -type f -print -quit | grep -q .; then
    printf '%s\n' 'release contains a symbolic link or special file' >&2
    return 1
  fi
}

mv "$incoming" "$staged"

# The deploy account cannot enter staging, so validation and copying happen
# after its upload has been removed from its writable tree.
if ! validate_release_tree "$staged"; then
  rm -rf "$staged"
  exit 1
fi

mkdir -m 0700 "$new"
if ! cp -R --no-dereference "$staged/." "$new/"; then
  rm -rf "$new"
  exit 1
fi
if ! validate_release_tree "$new"; then
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
