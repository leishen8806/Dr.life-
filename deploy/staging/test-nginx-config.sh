#!/bin/sh
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
  -subj '/CN=staging-placeholder' \
  -keyout "$root/origin.key" -out "$root/origin.crt" >/dev/null 2>&1
docker run --rm \
  --add-host api:127.0.0.1 \
  --add-host buyer:127.0.0.1 \
  --add-host merchant:127.0.0.1 \
  --add-host admin:127.0.0.1 \
  -v "$(pwd)/deploy/staging/nginx/staging.conf:/etc/nginx/conf.d/default.conf:ro" \
  -v "$(pwd)/deploy/staging/nginx/cloudflare-real-ip.conf:/etc/nginx/conf.d/cloudflare-real-ip.conf:ro" \
  -v "$(pwd)/deploy/staging/nginx/proxy_params:/etc/nginx/proxy_params:ro" \
  -v "$root/origin.crt:/etc/nginx/tls/origin.crt:ro" \
  -v "$root/origin.key:/etc/nginx/tls/origin.key:ro" \
  nginx:1.29.3-alpine nginx -t
printf '%s\n' 'nginx staging config: PASS'
