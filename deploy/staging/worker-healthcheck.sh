#!/usr/bin/env bash
set -euo pipefail

# E-09 worker liveness: asynq v0.26 writes expiring server heartbeat entries
# to the `asynq:servers` ZSET. The score is the heartbeat expiry timestamp.
# This runs from the staging host and uses the queue container's redis-cli so
# the queue password never appears in a command argument or output.

compose_file=${STAGING_COMPOSE_FILE:-compose.yaml}
env_file=${STAGING_ENV_FILE:-.env}
project_dir=${STAGING_PROJECT_DIR:-$(cd -- "$(dirname "$0")" && pwd)}
interval=${WORKER_HEALTHCHECK_INTERVAL_SECONDS:-35}
[[ $interval =~ ^[1-9][0-9]*$ ]] || { printf '%s\n' 'WORKER_HEALTHCHECK_INTERVAL_SECONDS must be a positive integer' >&2; exit 64; }

die() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }

docker_compose() {
  if docker ps >/dev/null 2>&1; then
    docker compose "$@"
  else
    sudo docker compose "$@"
  fi
}

latest_expiry() {
  local now output
  now=$(date +%s)
  output="$(cd "$project_dir" && docker_compose --project-directory "$project_dir" --env-file "$env_file" -f "$compose_file" exec -T redis-queue sh -c 'redis-cli --no-auth-warning --raw -a "$REDIS_QUEUE_PASSWORD" ZREVRANGEBYSCORE asynq:servers +inf "$1" WITHSCORES' sh "$now")" || die 'could not query asynq server heartbeats'
  awk 'NR % 2 == 0 && $1 ~ /^[0-9]+$/ && $1 > max { max = $1 } END { if (max == "") exit 1; print max }' <<<"$output" || die 'no fresh asynq server heartbeat found in asynq:servers'
}

first="$(latest_expiry)"
printf 'PASS: fresh asynq server heartbeat observed (expiry score present)\n'
sleep "$interval"
second="$(latest_expiry)"
if (( second <= first )); then
  die 'asynq server heartbeat expiry did not advance during the observation window'
fi
printf 'PASS: asynq server heartbeat advanced during %ss observation\n' "$interval"
