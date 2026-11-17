#!/bin/sh
set -eu
umask 077
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH

usage() { printf '%s\n' 'usage: admin-bootstrap.sh --username <name>' >&2; exit 2; }
[ "${1:-}" = "--username" ] && [ -n "${2:-}" ] && [ "$#" -eq 2 ] || usage
username=$2
compose_file=${STAGING_COMPOSE_FILE:-deploy/staging/compose.yaml}
env_file=${STAGING_ENV_FILE:-.env}
docker_bin=${DOCKER_BIN:-docker}
bootstrap_role="drlife_admininit_$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
bootstrap_password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
cleanup() {
  if ! "$docker_bin" compose --env-file "$env_file" -f "$compose_file" exec -T postgres psql -v ON_ERROR_STOP=1 -U "${POSTGRES_ADMIN_USER:-postgres}" -d "${POSTGRES_DB:-drlife_staging}" <<SQL >/dev/null 2>&1
DROP ROLE IF EXISTS "$bootstrap_role";
SQL
  then
    printf '%s\n' 'warning: failed to remove temporary admin bootstrap role' >&2
    exit 1
  fi
}
trap cleanup EXIT HUP INT TERM

"$docker_bin" compose --env-file "$env_file" -f "$compose_file" exec -T postgres psql -v ON_ERROR_STOP=1 -U "${POSTGRES_ADMIN_USER:-postgres}" -d "${POSTGRES_DB:-drlife_staging}" <<SQL
CREATE ROLE "$bootstrap_role" LOGIN PASSWORD '$bootstrap_password';
GRANT iam_rw TO "$bootstrap_role";
GRANT audit_writer TO "$bootstrap_role";
SQL

iam_database_url="postgres://${bootstrap_role}:${bootstrap_password}@postgres:5432/${POSTGRES_DB:-drlife_staging}?sslmode=disable"
export IAM_DATABASE_URL="$iam_database_url"
"$docker_bin" compose --env-file "$env_file" -f "$compose_file" run --rm --no-deps -T -e IAM_DATABASE_URL api /admininit super --username "$username"
