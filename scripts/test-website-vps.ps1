$ErrorActionPreference = 'Stop'
$root = Resolve-Path (Join-Path $PSScriptRoot '..')
$dist = Join-Path $root 'apps/website/dist'
$siteConfig = Join-Path $root 'deploy/nginx/settlecore.org.conf'
$headers = Join-Path $root 'deploy/nginx/settlecore-security-headers.conf'
$rollback = Join-Path $root 'deploy/website/rollback-current.sh'
$publish = Join-Path $root 'deploy/website/publish-release.sh'
$temp = Join-Path ([IO.Path]::GetTempPath()) ("settlecore-nginx-" + [guid]::NewGuid())

function Invoke-Docker { & docker @args; if ($LASTEXITCODE -ne 0) { throw 'docker command failed' } }
function Invoke-Curl { param([string[]]$CurlArgs); $output = & curl.exe @CurlArgs; if ($LASTEXITCODE -ne 0) { throw 'curl command failed' }; $output }
function Assert-Contains { param([string]$Output, [string]$Expected); if ($Output -notmatch [regex]::Escape($Expected)) { throw "missing expected output: $Expected`n$Output" } }

try {
  $first = '20261104T000001Z-first'; $second = '20261104T000002Z-second'; $third = '20261104T000003Z-third'
  New-Item -ItemType Directory -Path "$temp/site/releases/$first", "$temp/site/releases/$second", "$temp/site/releases/$third", "$temp/snippets" | Out-Null
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$first/"
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$second/"
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$third/"
  Set-Content -NoNewline "$temp/site/releases/$first/version.txt" 'first'
  Set-Content -NoNewline "$temp/site/releases/$second/version.txt" 'second'
  Set-Content -NoNewline "$temp/site/releases/$third/version.txt" 'third'
  Copy-Item $headers "$temp/snippets/settlecore-security-headers.conf"
  (Get-Content -Raw $siteConfig).Replace('listen 443 ssl;', 'listen 8080;').Replace('listen [::]:443 ssl;', 'listen [::]:8080;').Replace('ssl_certificate     /etc/nginx/tls/settlecore-origin.crt;', '').Replace('ssl_certificate_key /etc/nginx/tls/settlecore-origin.key;', '').Replace('ssl_protocols TLSv1.2 TLSv1.3;', '').Replace('/srv/settlecore/current', "/work/site/releases/$first").Replace('/etc/nginx/snippets/', '/work/snippets/') | Set-Content -NoNewline "$temp/settlecore.org.conf"
  @'
events {}
http { include /etc/nginx/mime.types; include /work/settlecore.org.conf; }
'@ | Set-Content -NoNewline "$temp/nginx.conf"
  Invoke-Docker run --rm --network none -v "${temp}:/work:ro" nginx:1.27-alpine nginx -c /work/nginx.conf -t
  Invoke-Docker run --rm -d --name settlecore-nginx-test -p 18080:80 -p 18081:8080 -v "${temp}:/work:ro" nginx:1.27-alpine nginx -c /work/nginx.conf -g 'daemon off;'
  Start-Sleep -Seconds 1
  $homeResponse = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', 'http://127.0.0.1:18081/')
  Write-Host $homeResponse
  Assert-Contains $homeResponse 'HTTP/1.1 200 OK'; Assert-Contains $homeResponse 'Content-Security-Policy:'; Assert-Contains $homeResponse 'Cache-Control: no-cache'
  $gzip = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', '-H', 'Accept-Encoding: gzip', 'http://127.0.0.1:18081/')
  Write-Host $gzip
  Assert-Contains $gzip 'Content-Encoding: gzip'
  $asset = (Get-ChildItem "$temp/site/releases/$first/_astro" -File | Select-Object -First 1).Name
  $assetHead = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', "http://127.0.0.1:18081/_astro/$asset")
  Write-Host $assetHead
  Assert-Contains $assetHead 'Cache-Control: public, max-age=31536000, immutable'; Assert-Contains $assetHead 'Content-Security-Policy:'
  $font = (Get-ChildItem "$temp/site/releases/$first/fonts" -Filter '*.woff2' -File | Select-Object -First 1).Name
  $fontHead = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', "http://127.0.0.1:18081/fonts/$font")
  Write-Host $fontHead
  Assert-Contains $fontHead 'Content-Security-Policy:'
  $missing = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', 'http://127.0.0.1:18081/_astro/missing.js')
  Write-Host $missing
  Assert-Contains $missing '404 Not Found'; Assert-Contains $missing 'Content-Security-Policy:'
  $redirect = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: www.settlecore.org', 'http://127.0.0.1:18080/terms?lang=km')
  Write-Host $redirect
  Assert-Contains $redirect '301 Moved Permanently'; Assert-Contains $redirect 'Location: https://settlecore.org/terms?lang=km'
  $privacy = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', 'http://127.0.0.1:18081/privacy')
  Write-Host $privacy
  Assert-Contains $privacy '301 Moved Permanently'; Assert-Contains $privacy 'Location: /privacy/'
  $directory = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', 'http://127.0.0.1:18081/fonts/')
  Write-Host $directory
  Assert-Contains $directory '404 Not Found'
  $hidden = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', 'http://127.0.0.1:18081/.env')
  Write-Host $hidden
  Assert-Contains $hidden '403 Forbidden'
  $rollbackCommand = 'set -eu; ln -s releases/' + $third + ' /srv/settlecore/current; sh /rollback-current.sh; test $(readlink /srv/settlecore/current) = releases/' + $second + '; sh /rollback-current.sh; test $(readlink /srv/settlecore/current) = releases/' + $first + '; if sh /rollback-current.sh; then exit 1; fi; test $(readlink /srv/settlecore/current) = releases/' + $first
  Invoke-Docker run --rm --network none -v "${temp}/site:/srv/settlecore" -v "${rollback}:/rollback-current.sh:ro" alpine:3.20 sh -c $rollbackCommand
  New-Item -ItemType Directory -Path "$temp/site/incoming/test-release" | Out-Null
  Set-Content -NoNewline "$temp/site/incoming/test-release/version.txt" 'published'
  Invoke-Docker run --rm --network none -v "${temp}/site:/srv/settlecore" -v "${publish}:/publish-release.sh:ro" alpine:3.20 sh -c 'set -eu; rm /srv/settlecore/current; sh /publish-release.sh test-release; test $(readlink /srv/settlecore/current) = releases/test-release; test -d /srv/settlecore/releases/test-release'
  Write-Host 'AC-2 and AC-3 container verification passed.'
} finally {
  docker rm -f settlecore-nginx-test 2>$null
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
