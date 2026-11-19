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
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output=$2; shift 2;;
    --write-out) shift 2;;
    *) shift;;
  esac
done
[ -n "$output" ] && printf '%s\n' '<html><body><div id="app">stub</div><script src="/assets/main.js"></script></body></html>' >"$output"
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
chmod +x "$root/bin/curl" "$root/bin/browser-stub"
: >"$root/browser.log"
PATH="$root/bin:$PATH" SMOKE_BROWSER_LOG="$root/browser.log" \
  SMOKE_API_BASE_URL=https://api.test SMOKE_BUYER_URL=https://buyer.test \
  SMOKE_MERCHANT_URL=https://merchant.test SMOKE_SHOP_URL=https://shop.test \
  SMOKE_BROWSER_COMMAND="$root/bin/browser-stub" "$script_dir/smoke.sh" >/dev/null
test "$(wc -l <"$root/browser.log" | tr -d ' ')" -eq 3
grep -q -- '--label buyer --url https://buyer.test' "$root/browser.log"
printf '%s\n' 'staging smoke browser contract: PASS'
