#!/usr/bin/env bash
set -euo pipefail

die() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
require_env() { local name="$1"; [[ -n "${!name:-}" ]] || die "$name is required"; }

for required in SMOKE_API_BASE_URL SMOKE_BUYER_URL SMOKE_MERCHANT_URL SMOKE_SHOP_URL; do
  require_env "$required"
done
command -v curl >/dev/null 2>&1 || die "curl is required"

normalise_url() { printf '%s' "${1%/}"; }

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

api_base="$(normalise_url "$SMOKE_API_BASE_URL")"
api_body="$(mktemp)"
request_200 "API health" "${api_base}/healthz" "$api_body"
rm -f "$api_body"
printf 'PASS: API %s/healthz returned HTTP 200\n' "$api_base"

check_frontend() {
  local label="$1" url="$2" body
  body="$(mktemp)"
  request_200 "$label" "$url" "$body"
  if ! grep -Eq "id=[\"'](app|root)[\"']" "$body"; then
    rm -f "$body"
    die "$label returned no app root"
  fi
  rm -f "$body"
  printf 'PASS: %s %s\n' "$label" "$url"
}

check_frontend "buyer" "$(normalise_url "$SMOKE_BUYER_URL")"
check_frontend "merchant" "$(normalise_url "$SMOKE_MERCHANT_URL")"
check_frontend "shop" "$(normalise_url "$SMOKE_SHOP_URL")"
printf 'PASS: staging smoke checks complete\n'
