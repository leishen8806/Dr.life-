$ErrorActionPreference = 'Stop'
$root = Resolve-Path (Join-Path $PSScriptRoot '..')
$dist = Join-Path $root 'apps/website/dist'
$config = Join-Path $root 'deploy/nginx/settlecore.org.conf'
$rollback = Join-Path $root 'deploy/website/rollback-current.sh'
$temp = Join-Path ([IO.Path]::GetTempPath()) ("settlecore-nginx-" + [guid]::NewGuid())

function Invoke-Docker {
  & docker @args
  if ($LASTEXITCODE -ne 0) { throw 'docker command failed' }
}

function Invoke-Curl {
  & curl.exe @args
  if ($LASTEXITCODE -ne 0) { throw 'curl command failed' }
}

try {
  New-Item -ItemType Directory -Path "$temp/tls", "$temp/site/releases/20261104T000001Z-first", "$temp/site/releases/20261104T000002Z-second" | Out-Null
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/20261104T000001Z-first/"
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/20261104T000002Z-second/"
  Set-Content -NoNewline "$temp/site/releases/20261104T000001Z-first/version.txt" 'first'
  Set-Content -NoNewline "$temp/site/releases/20261104T000002Z-second/version.txt" 'second'
  Invoke-Docker run --rm -v "${temp}:/work" alpine:3.20 sh -c 'apk add --no-cache openssl >/dev/null && openssl req -x509 -newkey rsa:2048 -nodes -keyout /work/tls/settlecore-origin.key -out /work/tls/settlecore-origin.crt -subj /CN=settlecore.org -days 1 >/dev/null 2>&1'
  (Get-Content -Raw $config).Replace('/srv/settlecore/current', '/work/site/releases/20261104T000001Z-first').Replace('/etc/nginx/tls/', '/work/tls/') | Set-Content -NoNewline "$temp/settlecore.org.conf"
  @'
events {}
  http { include /etc/nginx/mime.types; include /work/settlecore.org.conf; }
'@ | Set-Content -NoNewline "$temp/nginx.conf"
  Invoke-Docker run --rm -v "${temp}:/work:ro" fholzer/nginx-brotli:latest -c /work/nginx.conf -t
  Invoke-Docker run --rm -d --name settlecore-nginx-test -p 18080:80 -p 18443:443 -v "${temp}:/work:ro" fholzer/nginx-brotli:latest -c /work/nginx.conf -g 'daemon off;'
  Start-Sleep -Seconds 1
  Invoke-Docker exec settlecore-nginx-test nginx -t
  Invoke-Docker run --rm -v "${rollback}:/rollback-current.sh:ro" alpine:3.20 sh -c 'set -eu; mkdir -p /site/releases/20261104T000001Z-first /site/releases/20261104T000002Z-second; echo first >/site/releases/20261104T000001Z-first/version.txt; echo second >/site/releases/20261104T000002Z-second/version.txt; ln -s releases/20261104T000001Z-first /site/current; ln -s releases/20261104T000002Z-second /site/.current.next; mv -Tf /site/.current.next /site/current; grep -qx second /site/current/version.txt; SITE_ROOT=/site sh /rollback-current.sh; grep -qx first /site/current/version.txt; readlink /site/current'
  Invoke-Curl -skI --resolve settlecore.org:18443:127.0.0.1 https://settlecore.org:18443/
  Invoke-Curl -skI --resolve settlecore.org:18443:127.0.0.1 -H 'Accept-Encoding: br' https://settlecore.org:18443/
  $asset = (Get-ChildItem "$temp/site/releases/20261104T000001Z-first/_astro" -File | Select-Object -First 1).Name
  Invoke-Curl -skI --resolve settlecore.org:18443:127.0.0.1 "https://settlecore.org:18443/_astro/$asset"
  Invoke-Curl -skI --resolve settlecore.org:18443:127.0.0.1 https://settlecore.org:18443/_astro/missing.js
  Invoke-Curl -sI -H 'Host: www.settlecore.org' 'http://127.0.0.1:18080/terms?lang=km'
  Invoke-Curl -skI --resolve settlecore.org:18443:127.0.0.1 https://settlecore.org:18443/fonts/
  Invoke-Curl -skI --resolve settlecore.org:18443:127.0.0.1 https://settlecore.org:18443/.env
} finally {
  docker rm -f settlecore-nginx-test 2>$null
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
