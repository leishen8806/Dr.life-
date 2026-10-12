#!/usr/bin/env sh
set -eu

failed=0
report_matches() {
  label=$1
  pattern=$2
  matches=$(git grep -nI -E "$pattern" -- . ':!backend/scripts/check-secrets.sh' 2>/dev/null || true)
  if [ -n "$matches" ]; then
    printf '%s\n' "$matches" | awk -F: -v label="$label" '{print label ": " $1 ":" $2}' >&2
    failed=1
  fi
}

report_matches 'private key marker' 'BEGIN [A-Z ]*PRIVATE KEY'
report_matches 'AI key pattern' "s""k-[A-Za-z0-9_-]{20,}"
report_matches 'Telegram token pattern' '[0-9]{6,12}:[A-Za-z0-9_-]{35}'

tracked_env=$(git ls-files | awk '/(^|\/)\.env(\.|$)/ && $0 !~ /\.example$/')
if [ -n "$tracked_env" ]; then
  printf '%s\n' "$tracked_env" | awk '{print "tracked environment file: " $0}' >&2
  failed=1
fi

exit "$failed"
