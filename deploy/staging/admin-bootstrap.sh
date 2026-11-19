#!/bin/sh
set -eu
umask 077
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH

usage() {
  printf '%s\n' \
    'usage: admin-bootstrap.sh --username <name>' \
    '       admin-bootstrap.sh --bot buyer|merchant --bot-username <name> --actor-username <name>' >&2
  exit 2
}

mode=${1:-}
case "$mode" in
  --username)
    [ -n "${2:-}" ] && [ "$#" -eq 2 ] || usage
    username=$2
    ;;
  --bot)
    [ "$#" -eq 6 ] && [ -n "${2:-}" ] && [ "$3" = "--bot-username" ] && [ -n "${4:-}" ] && [ "$5" = "--actor-username" ] && [ -n "${6:-}" ] || usage
    bot_end=$2
    bot_username=$4
    actor_username=$6
    case "$bot_end" in
      buyer|merchant) ;;
      *) usage ;;
    esac
    IFS= read -r bot_token || {
      printf '%s\n' 'error: bot token is required on standard input' >&2
      exit 1
    }
    [ -n "$bot_token" ] || {
      printf '%s\n' 'error: bot token is required on standard input' >&2
      exit 1
    }
    bot_token=$(printf '%s' "$bot_token" | tr -d '\r')
    ;;
  *)
    usage
    ;;
esac
compose_file=${STAGING_COMPOSE_FILE:-compose.yaml}
env_file=${STAGING_ENV_FILE:-.env}
docker_bin=${DOCKER_BIN:-docker}
sudo_bin=${SUDO_BIN:-sudo}
docker_cmd() {
  if [ "${DOCKER_USE_SUDO:-0}" = 1 ]; then
    "$sudo_bin" --preserve-env=IAM_DATABASE_URL "$docker_bin" "$@"
  else
    "$docker_bin" "$@"
  fi
}
bootstrap_role="drlife_admininit_$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
bootstrap_password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
cleanup() {
  if ! docker_cmd compose --env-file "$env_file" -f "$compose_file" exec -T postgres psql -v ON_ERROR_STOP=1 -U "${POSTGRES_ADMIN_USER:-postgres}" -d "${POSTGRES_DB:-drlife_staging}" <<SQL >/dev/null 2>&1
DROP ROLE IF EXISTS "$bootstrap_role";
SQL
  then
    printf '%s\n' 'warning: failed to remove temporary admin bootstrap role' >&2
    exit 1
  fi
}
trap cleanup EXIT HUP INT TERM

docker_cmd compose --env-file "$env_file" -f "$compose_file" exec -T postgres psql -v ON_ERROR_STOP=1 -U "${POSTGRES_ADMIN_USER:-postgres}" -d "${POSTGRES_DB:-drlife_staging}" <<SQL
CREATE ROLE "$bootstrap_role" LOGIN PASSWORD '$bootstrap_password';
GRANT iam_rw TO "$bootstrap_role";
GRANT audit_writer TO "$bootstrap_role";
SQL

iam_database_url="postgres://${bootstrap_role}:${bootstrap_password}@postgres:5432/${POSTGRES_DB:-drlife_staging}?sslmode=disable"
export IAM_DATABASE_URL="$iam_database_url"
if [ "$mode" = "--username" ]; then
  docker_cmd compose --env-file "$env_file" -f "$compose_file" run --rm --no-deps -T --entrypoint /admininit -e IAM_DATABASE_URL api super --username "$username"
else
  actor_public_ids=$(docker_cmd compose --env-file "$env_file" -f "$compose_file" exec -T postgres psql -At -v ON_ERROR_STOP=1 -v actor_username="$actor_username" -U "${POSTGRES_ADMIN_USER:-postgres}" -d "${POSTGRES_DB:-drlife_staging}" <<'SQL'
SELECT public_id FROM iam.admin_user WHERE name=:'actor_username' AND role='super' AND status='active' LIMIT 2;
SQL
  )
  [ -n "$actor_public_ids" ] || {
    printf '%s\n' 'error: exactly one active super administrator was required' >&2
    exit 1
  }
  actor_public_id=$(printf '%s\n' "$actor_public_ids" | sed -n '1p')
  actor_extra=$(printf '%s\n' "$actor_public_ids" | sed -n '2p')
  [ -z "$actor_extra" ] || {
    printf '%s\n' 'error: exactly one active super administrator was required' >&2
    exit 1
  }
  printf '%s\n' "$bot_token" | docker_cmd compose --env-file "$env_file" -f "$compose_file" run --rm --no-deps -T --entrypoint /admininit -e IAM_DATABASE_URL api bot --end "$bot_end" --username "$bot_username" --actor "$actor_public_id"
fi
