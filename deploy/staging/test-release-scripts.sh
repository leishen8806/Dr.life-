#!/bin/sh
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
script_dir=$(cd -- "$(dirname "$0")" && pwd)
for app in buyer merchant admin; do
  mkdir -p "$root/$app/incoming/r1" "$root/$app/releases"
  printf '%s\n' "$app" > "$root/$app/incoming/r1/index.html"
  STAGING_SITE_ROOT="$root" "$script_dir/publish-release.sh" "$app" r1
  test "$(readlink "$root/$app/current")" = "releases/r1"
  mkdir -p "$root/$app/incoming/r2"
  printf '%s\n' "$app-2" > "$root/$app/incoming/r2/index.html"
  STAGING_SITE_ROOT="$root" "$script_dir/publish-release.sh" "$app" r2
  test "$(readlink "$root/$app/current")" = "releases/r2"
  STAGING_SITE_ROOT="$root" "$script_dir/rollback-release.sh" "$app"
  test "$(readlink "$root/$app/current")" = "releases/r1"
done
echo "release scripts dry-run: PASS"
