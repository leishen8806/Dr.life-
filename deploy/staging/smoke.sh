#!/usr/bin/env bash
set -euo pipefail

die() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
require_env() { local name="$1"; [[ -n "${!name:-}" ]] || die "$name is required"; }

for required in SMOKE_API_BASE_URL SMOKE_BUYER_URL SMOKE_MERCHANT_URL SMOKE_SHOP_URL; do
  require_env "$required"
done
require_env SMOKE_BROWSER_COMMAND
command -v curl >/dev/null 2>&1 || die "curl is required"

normalise_url() { printf '%s' "${1%/}"; }

check_api_image_release() {
  local env_file compose_file expected container_id actual
  env_file=${SMOKE_ENV_FILE:-${STAGING_ENV_FILE:-.env}}
  [[ -f "$env_file" ]] || return 0
  compose_file=${SMOKE_COMPOSE_FILE:-${STAGING_COMPOSE_FILE:-compose.yaml}}
  [[ -f "$compose_file" ]] || die "staging compose file is missing: $compose_file"
  expected="$(awk -F= '$1 == "APP_RELEASE" { value=$2; sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+#.*$/, "", value); gsub(/^"|"$/, "", value); print value; exit }' "$env_file")"
  [[ -n "$expected" ]] || die "APP_RELEASE is required in $env_file"
  if [[ -n "${DOCKER_BIN:-}" ]]; then
    docker_cmd=("$DOCKER_BIN")
  elif docker ps >/dev/null 2>&1; then
    docker_cmd=(docker)
  else
    docker_cmd=(sudo docker)
  fi
  container_id="$("${docker_cmd[@]}" compose --env-file "$env_file" -f "$compose_file" ps -q api | sed -n '1p')"
  [[ -n "$container_id" ]] || die 'running api container is required for image release check'
  actual="$("${docker_cmd[@]}" inspect --format '{{.Config.Image}}' "$container_id")"
  [[ "$actual" == "drlife-api:$expected" ]] || die "running api image $actual does not match APP_RELEASE $expected"
  printf 'PASS: running api image matches APP_RELEASE %s\n' "$expected"
}

request_200() {
  local label="$1" url="$2" body="$3" attempt status
  for ((attempt = 1; attempt <= 3; attempt++)); do
    if status="$(curl --silent --show-error --location --max-time 10 \
      --output "$body" --write-out '%{http_code}' "$url")" && [[ "$status" == "200" ]]; then
      return 0
    fi
    sleep 1
  done
  die "$label did not return HTTP 200"
}

check_public_categories() {
  local label="$1" url="$2" body status
  body="$(mktemp)"
  status="$(curl --silent --show-error --location --max-time 10 \
    --output "$body" --write-out '%{http_code}' "$url")" || true
  if [[ "$status" != "200" ]]; then
    rm -f "$body"
    die "$label did not return HTTP 200 (got $status)"
  fi
  if ! grep -Eq '"items"[[:space:]]*:[[:space:]]*\[' "$body"; then
    rm -f "$body"
    die "$label returned HTTP 200 but not an items array"
  fi
  rm -f "$body"
  printf 'PASS: %s returned HTTP 200 with an items array\n' "$label"
}

check_empty_products() {
  local label="$1" url="$2" body status
  body="$(mktemp)"
  status="$(curl --silent --show-error --location --max-time 10 \
    --output "$body" --write-out '%{http_code}' "$url")" || true
  if [[ "$status" != "200" ]]; then
    rm -f "$body"
    die "$label did not return HTTP 200 (got $status)"
  fi
  if ! grep -Eq '"items"[[:space:]]*:[[:space:]]*\[\][[:space:]]*[,}]' "$body" || ! grep -Eq '"total"[[:space:]]*:[[:space:]]*0([,}])' "$body"; then
    rm -f "$body"
    die "$label returned HTTP 200 but not an empty list"
  fi
  rm -f "$body"
  printf 'PASS: %s returned HTTP 200 with an empty list\n' "$label"
}

api_base="$(normalise_url "$SMOKE_API_BASE_URL")"
check_api_image_release
api_body="$(mktemp)"
request_200 "API health" "${api_base}/healthz" "$api_body"
rm -f "$api_body"
printf 'PASS: API %s/healthz returned HTTP 200\n' "$api_base"
if [[ "${SMOKE_EXPECT_EMPTY_CATALOG:-}" == "1" ]]; then
  check_public_categories "public categories" "${api_base}/v1/public/categories"
  check_empty_products "public products" "${api_base}/v1/public/products?limit=1"
fi

check_frontend() {
  local label="$1" url="$2" body
  body="$(mktemp)"
  request_200 "$label" "$url" "$body"
  if ! grep -Eq "id=[\"'](app|root)[\"']" "$body"; then
    rm -f "$body"
    die "$label returned no app root"
  fi
  rm -f "$body"
  "$SMOKE_BROWSER_COMMAND" --label "$label" --url "$url" || die "$label browser render check failed"
  printf 'PASS: %s %s\n' "$label" "$url"
}

check_frontend "buyer" "$(normalise_url "$SMOKE_BUYER_URL")"
check_frontend "merchant" "$(normalise_url "$SMOKE_MERCHANT_URL")"
check_frontend "shop" "$(normalise_url "$SMOKE_SHOP_URL")"
printf 'PASS: staging smoke checks complete\n'
