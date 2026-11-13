#!/usr/bin/env bash
set -euo pipefail

die() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
require_env() { local name="$1"; [[ -n "${!name:-}" ]] || die "$name is required"; }

for required in SMOKE_API_BASE_URL SMOKE_BUYER_URL SMOKE_MERCHANT_URL SMOKE_SHOP_URL; do
  require_env "$required"
done
command -v curl >/dev/null 2>&1 || die "curl is required"

normalise_url() { printf '%s' "${1%/}"; }

check_url() {
  local label="$1" url="$2" body attempt
  body="$(mktemp)"
  trap 'rm -f "$body"' RETURN
  for ((attempt = 1; attempt <= 3; attempt++)); do
    if curl --fail --silent --show-error --location --max-time 10 --output "$body" "$url"; then
      if grep -Eq "id=[\"'](app|root)[\"']|<main([[:space:]>])" "$body"; then
        printf 'PASS: %s %s\n' "$label" "$url"
        return 0
      fi
      die "$label returned no app root"
    fi
    sleep 1
  done
  die "$label did not return HTTP 200"
}

api_base="$(normalise_url "$SMOKE_API_BASE_URL")"
api_body="$(mktemp)"
trap 'rm -f "$api_body"' EXIT
curl --fail --silent --show-error --location --max-time 10 --output "$api_body" "${api_base}/healthz" || die "API health check failed"
printf 'PASS: API %s/healthz returned HTTP 200\n' "$api_base"
check_url "buyer" "$(normalise_url "$SMOKE_BUYER_URL")"
check_url "merchant" "$(normalise_url "$SMOKE_MERCHANT_URL")"
check_url "shop" "$(normalise_url "$SMOKE_SHOP_URL")"
printf 'PASS: staging smoke checks complete\n'
