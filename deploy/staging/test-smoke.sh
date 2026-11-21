#!/bin/sh
set -eu
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
mkdir -p "$root/bin"
cat >"$root/bin/curl" <<'EOF'
#!/bin/sh
set -eu
output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output=$2; shift 2;;
    --url) url=$2; shift 2;;
    --write-out) shift 2;;
    http*) url=$1; shift;;
    *) shift;;
  esac
done
[ -n "$output" ] && case "$url" in
  *'/v1/public/categories'*|*'/v1/public/products'*) printf '%s\n' '{"ok":true,"data":{"items":[],"total":0},"request_id":"smoke"}' >"$output" ;;
  *) printf '%s\n' '<html><body><div id="app">stub</div><script src="/assets/main.js"></script></body></html>' >"$output" ;;
esac
printf '200'
EOF
cat >"$root/bin/browser-stub" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$SMOKE_BROWSER_LOG"
case "$*" in
  *"--label buyer"*"--url https://buyer.test"*|*"--label merchant"*"--url https://merchant.test"*|*"--label shop"*"--url https://shop.test"*) ;;
  *) exit 1;;
esac
EOF
cat >"$root/bin/docker" <<'EOF'
#!/bin/sh
set -eu
case "${1:-}" in
  ps)
    if [ "${FAKE_DOCKER_NO_SOCKET:-}" = 1 ]; then
      exit 1
    fi
    exit 0
    ;;
  compose)
    shift
    case " $* " in
      *" ps -q api "*) printf '%s\n' api-container ;;
      *) exit 0 ;;
    esac
    ;;
  inspect)
    printf '%s\n' "${FAKE_API_IMAGE:-drlife-api:test-release}"
    ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$root/bin/curl" "$root/bin/browser-stub" "$root/bin/docker"
: >"$root/compose.yaml"
printf '%s\n' 'APP_RELEASE=test-release' >"$root/env"
: >"$root/browser.log"
PATH="$root/bin:$PATH" DOCKER_BIN="$root/bin/docker" SMOKE_ENV_FILE="$root/env" \
  SMOKE_COMPOSE_FILE="$root/compose.yaml" SMOKE_BROWSER_LOG="$root/browser.log" \
  SMOKE_EXPECT_EMPTY_CATALOG=1 \
  SMOKE_API_BASE_URL=https://api.test SMOKE_BUYER_URL=https://buyer.test \
  SMOKE_MERCHANT_URL=https://merchant.test SMOKE_SHOP_URL=https://shop.test \
  SMOKE_BROWSER_COMMAND="$root/bin/browser-stub" "$script_dir/smoke.sh" >/dev/null
test "$(wc -l <"$root/browser.log" | tr -d ' ')" -eq 3
grep -q -- '--label buyer --url https://buyer.test' "$root/browser.log"
if PATH="$root/bin:$PATH" DOCKER_BIN="$root/bin/docker" SMOKE_ENV_FILE="$root/env" \
  SMOKE_COMPOSE_FILE="$root/compose.yaml" SMOKE_BROWSER_LOG="$root/browser.log" \
  FAKE_API_IMAGE=drlife-api:old SMOKE_API_BASE_URL=https://api.test \
  SMOKE_BUYER_URL=https://buyer.test SMOKE_MERCHANT_URL=https://merchant.test \
  SMOKE_SHOP_URL=https://shop.test SMOKE_BROWSER_COMMAND="$root/bin/browser-stub" \
  "$script_dir/smoke.sh" >/dev/null 2>"$root/mismatch.log"; then
  printf '%s\n' 'expected image mismatch to fail' >&2
  exit 1
fi
grep -q 'does not match APP_RELEASE test-release' "$root/mismatch.log"
printf '%s\r\n' "export APP_RELEASE = 'test-release'" >"$root/variant-env"
PATH="$root/bin:$PATH" DOCKER_BIN="$root/bin/docker" SMOKE_ENV_FILE="$root/variant-env" \
  SMOKE_COMPOSE_FILE="$root/compose.yaml" SMOKE_BROWSER_LOG="$root/browser.log" \
  SMOKE_REQUIRE_RELEASE_CHECK=1 SMOKE_API_BASE_URL=https://api.test \
  SMOKE_BUYER_URL=https://buyer.test SMOKE_MERCHANT_URL=https://merchant.test \
  SMOKE_SHOP_URL=https://shop.test SMOKE_BROWSER_COMMAND="$root/bin/browser-stub" \
  "$script_dir/smoke.sh" >/dev/null

if PATH="$root/bin:$PATH" DOCKER_BIN="$root/bin/docker" SMOKE_ENV_FILE="$root/missing.env" \
  SMOKE_COMPOSE_FILE="$root/compose.yaml" SMOKE_BROWSER_LOG="$root/browser.log" \
  SMOKE_REQUIRE_RELEASE_CHECK=1 SMOKE_API_BASE_URL=https://api.test \
  SMOKE_BUYER_URL=https://buyer.test SMOKE_MERCHANT_URL=https://merchant.test \
  SMOKE_SHOP_URL=https://shop.test SMOKE_BROWSER_COMMAND="$root/bin/browser-stub" \
  "$script_dir/smoke.sh" >/dev/null 2>"$root/missing-env.log"; then
  printf '%s\n' 'expected missing env release check to fail' >&2
  exit 1
fi
grep -q 'APP_RELEASE check requires env file' "$root/missing-env.log"

cat >"$root/bin/sudo" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$SUDO_LOG"
[ "${1:-}" = -n ] && [ "${2:-}" = docker ] || exit 1
shift 2
exec "$FAKE_DOCKER" "$@"
EOF
chmod +x "$root/bin/sudo"
: >"$root/sudo.log"
PATH="$root/bin:$PATH" FAKE_DOCKER="$root/bin/docker" FAKE_DOCKER_NO_SOCKET=1 \
  SUDO_LOG="$root/sudo.log" SMOKE_ENV_FILE="$root/env" \
  SMOKE_COMPOSE_FILE="$root/compose.yaml" SMOKE_BROWSER_LOG="$root/browser.log" \
  SMOKE_REQUIRE_RELEASE_CHECK=1 SMOKE_API_BASE_URL=https://api.test \
  SMOKE_BUYER_URL=https://buyer.test SMOKE_MERCHANT_URL=https://merchant.test \
  SMOKE_SHOP_URL=https://shop.test SMOKE_BROWSER_COMMAND="$root/bin/browser-stub" \
  "$script_dir/smoke.sh" >/dev/null
grep -q '^-n docker compose ' "$root/sudo.log"
printf '%s\n' 'staging smoke browser contract: PASS'
