#!/usr/bin/env bash
set -euo pipefail

# E-09.4: custom-format PostgreSQL backup, restore into a caller-supplied
# drlife_restore_drill_* database, compare schema/table row counts, and always
# drop the temporary database on exit. Credentials stay in the two URL env vars.

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
[[ "$restore_db" =~ ^[A-Za-z0-9_]+$ ]] || die "restore database contains unsafe characters"
source_db="$(database_name_from_url "$SOURCE_DATABASE_URL")" || die "SOURCE_DATABASE_URL must be a PostgreSQL URI with a database name"

connection_key_from_url() {
  local url="$1" authority host port
  authority="${url#*://}"
  authority="${authority%%/*}"
  authority="${authority##*@}"
  [[ -n "$authority" ]] || return 1
  if [[ "$authority" == *:* ]]; then
    port="${authority##*:}"
    host="${authority%:*}"
  else
    host="$authority"
    port="5432"
  fi
  printf '%s|%s' "$host" "$port"
}

maintenance_url_from_url() {
  local url="$1" base query
  base="${url%%\?*}"
  query=""
  [[ "$url" == *\?* ]] && query="?${url#*\?}"
  printf '%s/postgres%s' "${base%/*}" "$query"
}

source_key="$(connection_key_from_url "$SOURCE_DATABASE_URL")" || die "SOURCE_DATABASE_URL must be a PostgreSQL URI"
restore_key="$(connection_key_from_url "$RESTORE_DATABASE_URL")" || die "RESTORE_DATABASE_URL must be a PostgreSQL URI"
[[ "$source_key|$source_db" != "$restore_key|$restore_db" ]] || die "source and restore database endpoints must differ"

drop_restore_database() {
  local maintenance_url
  maintenance_url="$(maintenance_url_from_url "$RESTORE_DATABASE_URL")"
  psql --no-psqlrc --set ON_ERROR_STOP=1 --set dbname="$restore_db" "$maintenance_url" <<'SQL'
SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE);', :'dbname') \gexec
SQL
}

create_restore_database() {
  local maintenance_url
  maintenance_url="$(maintenance_url_from_url "$RESTORE_DATABASE_URL")"
  psql --no-psqlrc --set ON_ERROR_STOP=1 --set dbname="$restore_db" "$maintenance_url" <<'SQL'
SELECT format('CREATE DATABASE %I;', :'dbname') \gexec
SQL
}

table_counts() {
  local database_url="$1" output_file="$2"
  psql --no-psqlrc --tuples-only --no-align --field-separator='|' \
    --set ON_ERROR_STOP=1 --dbname="$database_url" <<'SQL' >"$output_file"
SELECT format(
  'SELECT %L, %L, count(*) FROM %I.%I;',
  table_schema, table_name, table_schema, table_name
)
FROM information_schema.tables
WHERE table_type = 'BASE TABLE'
  AND table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY table_schema, table_name;
\gexec
SQL
}

if [[ "${1:-}" == "--dry-run" ]]; then
  printf 'PASS: dry-run validated source endpoint differs from safe target %s\n' "$restore_db"
  printf 'NOT_RUN: pg_dump, restore, comparison, and cleanup (dry-run)\n'
  exit 0
fi
[[ "${1:-}" == "" ]] || die "usage: $0 [--dry-run]"

for command in pg_dump pg_restore psql diff paste awk; do
  command -v "$command" >/dev/null 2>&1 || die "$command is required"
done
cleanup() {
  rm -f "$DUMP_FILE" "${DUMP_FILE}.source-counts" "${DUMP_FILE}.restore-counts" "${DUMP_FILE}.comparison"
  if ! drop_restore_database >/dev/null 2>&1; then
    printf 'FAIL: temporary restore database cleanup failed\n' >&2
  fi
}
trap cleanup EXIT

printf 'PASS: source backup started (read-only pg_dump)\n'
pg_dump --format=custom --file="$DUMP_FILE" --dbname="$SOURCE_DATABASE_URL"
drop_restore_database
create_restore_database
pg_restore --exit-on-error --dbname="$RESTORE_DATABASE_URL" "$DUMP_FILE"

source_counts="${DUMP_FILE}.source-counts"
restore_counts="${DUMP_FILE}.restore-counts"
comparison="${DUMP_FILE}.comparison"
table_counts "$SOURCE_DATABASE_URL" "$source_counts"
table_counts "$RESTORE_DATABASE_URL" "$restore_counts"
if ! diff -u "$source_counts" "$restore_counts" >/dev/null; then
  die "schema/table row counts differ (see $source_counts and $restore_counts)"
fi
paste -d '|' "$source_counts" "$restore_counts" | awk -F'|' '{print $1 "|" $2 "|" $3 "|" $6}' >"$comparison"
printf 'PASS: schema|table|source_rows|restore_rows\n'
cat "$comparison"
printf 'PASS: restore drill complete; temporary database will be dropped\n'
