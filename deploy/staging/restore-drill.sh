#!/usr/bin/env bash
set -euo pipefail

readonly RESTORE_PREFIX="drlife_restore_drill_"
dump_dir="${TMPDIR:-/tmp}"
DUMP_FILE="${dump_dir}/${RESTORE_PREFIX}$(date -u +%Y%m%dT%H%M%SZ).dump"
readonly DUMP_FILE
die() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
[[ -n "${SOURCE_DATABASE_URL:-}" ]] || die "SOURCE_DATABASE_URL is required"
[[ -n "${RESTORE_DATABASE_URL:-}" ]] || die "RESTORE_DATABASE_URL is required"

database_name_from_url() {
  local url="$1"
  local path="${url#*/}"
  path="${path%%\?*}"
  [[ "$path" != "$url" && -n "$path" ]] || return 1
  printf '%s' "${path##*/}"
}

restore_db="$(database_name_from_url "$RESTORE_DATABASE_URL")" || die "RESTORE_DATABASE_URL must be a PostgreSQL URI with a database name"
[[ "$restore_db" == "${RESTORE_PREFIX}"* ]] || die "restore database must start with ${RESTORE_PREFIX}"

if [[ "${1:-}" == "--dry-run" ]]; then
  printf 'PASS: dry-run validated source database and safe target %s\n' "$restore_db"
  printf 'NOT_RUN: pg_dump, restore, comparison, and cleanup (dry-run)\n'
  exit 0
fi
[[ "${1:-}" == "" ]] || die "usage: $0 [--dry-run]"

for command in pg_dump pg_restore psql dropdb createdb; do
  command -v "$command" >/dev/null 2>&1 || die "$command is required"
done
cleanup() {
  rm -f "$DUMP_FILE"
  dropdb --if-exists --force "$RESTORE_DATABASE_URL" >/dev/null 2>&1 || true
}
trap cleanup EXIT

printf 'PASS: source backup started (read-only pg_dump)\n'
pg_dump --format=custom --file="$DUMP_FILE" --dbname="$SOURCE_DATABASE_URL"
dropdb --if-exists --force "$RESTORE_DATABASE_URL"
createdb "$RESTORE_DATABASE_URL"
pg_restore --exit-on-error --dbname="$RESTORE_DATABASE_URL" "$DUMP_FILE"

schema_query="SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema');"
row_query="SELECT COALESCE(sum(n_live_tup), 0)::bigint FROM pg_stat_user_tables;"
source_schema="$(psql --no-psqlrc --tuples-only --no-align --dbname="$SOURCE_DATABASE_URL" --command="$schema_query")"
restore_schema="$(psql --no-psqlrc --tuples-only --no-align --dbname="$RESTORE_DATABASE_URL" --command="$schema_query")"
source_rows="$(psql --no-psqlrc --tuples-only --no-align --dbname="$SOURCE_DATABASE_URL" --command="$row_query")"
restore_rows="$(psql --no-psqlrc --tuples-only --no-align --dbname="$RESTORE_DATABASE_URL" --command="$row_query")"
[[ "$source_schema" == "$restore_schema" ]] || die "schema table count differs"
[[ "$source_rows" == "$restore_rows" ]] || die "row count differs"
printf 'PASS: schema tables=%s rows=%s\n' "$restore_schema" "$restore_rows"
printf 'PASS: restore drill complete; temporary database will be dropped\n'
