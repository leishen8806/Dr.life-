#!/usr/bin/env bash
set -euo pipefail

origin="${WEBSITE_ORIGIN:?set WEBSITE_ORIGIN}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

check_page() {
  local path="$1"
  local expected_title="$2"
  local url="${origin}${path}"
  local body="$tmp_dir/body-${path//\//_}"
  local status

  status="$(curl --silent --show-error --location --retry 5 --retry-delay 5 --retry-max-time 110 \
    --connect-timeout 10 --max-time 120 -H 'Cache-Control: no-cache' \
    -o "$body" -w '%{http_code}' "$url")"
  if [ "$status" != 200 ]; then
    echo "::error::${url}: HTTP status ${status}, expected 200"
    return 1
  fi

  python3 - "$url" "$body" "$expected_title" <<'PY'
import html
import re
import sys

url, body_path, expected_title = sys.argv[1:]
document = open(body_path, encoding="utf-8").read()
title_match = re.search(r"<title(?:\s[^>]*)?>(.*?)</title>", document, re.I | re.S)
title = html.unescape(re.sub(r"\s+", " ", title_match.group(1))).strip() if title_match else ""
if title != expected_title:
    print(f"::error::{url}: title mismatch; expected {expected_title!r}, got {title!r}")
    raise SystemExit(1)
if not re.search(r'<meta\b[^>]+(?:property|name)=["\']og:image["\'][^>]*>', document, re.I):
    print(f"::error::{url}: missing og:image")
    raise SystemExit(1)
if not re.search(r'<link\b[^>]+hreflang=["\']x-default["\'][^>]*>', document, re.I):
    print(f"::error::{url}: missing hreflang x-default")
    raise SystemExit(1)
print(f"PASS {url}: HTTP 200, title, og:image, hreflang x-default")
PY
}

check_page "/" "DR.LIFE+ 生活家｜金边建材家居与中柬跨境物流平台 - DR集团"
check_page "/en/" "DR.LIFE+ | Building Materials & Home Platform in Phnom Penh, Cambodia - DR Group"
check_page "/km/" "DR.LIFE+ | វេទិកាសម្ភារៈសំណង់ និងគ្រឿងសង្ហារិមនៅភ្នំពេញ - DR Group"
check_page "/merchant/" "商家入驻 DR.LIFE+｜金边线上建材店 0% 平台佣金 - DR集团"
