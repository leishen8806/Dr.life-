$ErrorActionPreference = 'Stop'
$root = Resolve-Path (Join-Path $PSScriptRoot '..')
$dist = Join-Path $root 'apps/website/dist'
$siteConfig = Join-Path $root 'deploy/nginx/settlecore.org.conf'
$headers = Join-Path $root 'deploy/nginx/settlecore-security-headers.conf'
$rollback = Join-Path $root 'deploy/website/rollback-current.sh'
$publish = Join-Path $root 'deploy/website/publish-release.sh'
$releaseGuide = Join-Path $root 'docs/ops/website-release.md'
$dockerExe = 'C:\Program Files\Docker\Docker\resources\bin\docker.exe'
$temp = Join-Path ([IO.Path]::GetTempPath()) ("settlecore-nginx-" + [guid]::NewGuid())

function Invoke-Docker { & $dockerExe @args; if ($LASTEXITCODE -ne 0) { throw 'docker command failed' } }
function Invoke-Curl { param([string[]]$CurlArgs); $output = & curl.exe @CurlArgs; if ($LASTEXITCODE -ne 0) { throw 'curl command failed' }; $output }
function Assert-Contains { param([string]$Output, [string]$Expected); if ($Output -notmatch [regex]::Escape($Expected)) { throw "missing expected output: $Expected`n$Output" } }
function Assert-Equal { param([string]$Actual, [string]$Expected); if ($Actual -cne $Expected) { throw "expected: $Expected`nactual: $Actual" } }
function Test-GuideSudoersCase {
  param([string]$Pipeline, [string]$SudoList)
  $encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($SudoList))
  & $dockerExe run --rm --network none alpine:3.20 sh -c "printf '%s' $encoded | base64 -d | $Pipeline" *> $null
  return $LASTEXITCODE -eq 0
}

try {
  Assert-Equal (Get-Content -LiteralPath $publish -TotalCount 1) '#!/bin/sh'
  Assert-Equal (Get-Content -LiteralPath $rollback -TotalCount 1) '#!/bin/sh'
  Assert-Contains (Get-Content -Raw -LiteralPath $siteConfig) 'disable_symlinks on from=$document_root;'
  $guideCommand = Get-Content -LiteralPath $releaseGuide | Where-Object { $_ -like 'sudo -l -U settlecore-deploy*' }
  Assert-Equal ([string]$guideCommand.Count) '1'
  $guidePipeline = $guideCommand.Substring($guideCommand.IndexOf('|') + 1).Trim()
  $securePath = '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'
  $sudoCases = @(
    @{ Name = 'effective'; Output = "Matching Defaults entries for settlecore-deploy on host:`n    env_reset, mail_badpass, secure_path=$($securePath -replace ':', '\\:'), use_pty"; Expected = $true }
    @{ Name = 'commented'; Output = "# secure_path=$securePath"; Expected = $false }
    @{ Name = 'disabled'; Output = 'Defaults !secure_path'; Expected = $false }
    @{ Name = 'missing'; Output = 'Matching Defaults entries for settlecore-deploy on host:'; Expected = $false }
  )
  foreach ($case in $sudoCases) {
    Assert-Equal ([string](Test-GuideSudoersCase $guidePipeline $case.Output)) ([string]$case.Expected)
  }
  Write-Host 'R1 guide-command sudoers fixture verification passed.'
  $first = '20261104T000001Z-first'; $second = '20261104T000002Z-second'; $third = '20261104T000003Z-third'
  New-Item -ItemType Directory -Path "$temp/site/releases/$first", "$temp/site/releases/$second", "$temp/site/releases/$third", "$temp/snippets" | Out-Null
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$first/"
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$second/"
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$third/"
  Set-Content -NoNewline -LiteralPath "$temp/site/releases/$first/version.txt" -Value 'first'
  Set-Content -NoNewline -LiteralPath "$temp/site/releases/$second/version.txt" -Value 'second'
  Set-Content -NoNewline -LiteralPath "$temp/site/releases/$third/version.txt" -Value 'third'
  Set-Content -NoNewline -LiteralPath "$temp/site/escape.txt" -Value 'outside-root'
  Invoke-Docker run --rm --network none -v "${temp}:/work" alpine:3.20 ln -s ../../escape.txt "/work/site/releases/$first/escape.txt"
  Copy-Item $headers "$temp/snippets/settlecore-security-headers.conf"
  (Get-Content -Raw $siteConfig).Replace('listen 443 ssl;', 'listen 8080;').Replace('listen [::]:443 ssl;', 'listen [::]:8080;').Replace('ssl_certificate     /etc/nginx/tls/settlecore-origin.crt;', '').Replace('ssl_certificate_key /etc/nginx/tls/settlecore-origin.key;', '').Replace('ssl_protocols TLSv1.2 TLSv1.3;', '').Replace('/srv/settlecore/current', "/work/site/releases/$first").Replace('/etc/nginx/snippets/', '/work/snippets/') | Set-Content -NoNewline -LiteralPath "$temp/settlecore.org.conf"
  @'
events {}
http { include /etc/nginx/mime.types; include /work/settlecore.org.conf; }
'@ | Set-Content -NoNewline -LiteralPath "$temp/nginx.conf"
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
  $symlink = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: settlecore.org', 'http://127.0.0.1:18081/escape.txt')
  Write-Host $symlink
  if ($symlink -match '200 OK') { throw 'nginx followed a release symlink outside the document root' }
  $rollbackCommand = 'set -eu; mkdir -p /tmp/test-bin; printf "#!/bin/sh\nexit 66\n" >/tmp/test-bin/find; chmod 0755 /tmp/test-bin/find; ln -s releases/' + $third + ' /srv/settlecore/current; ln -s releases/' + $first + ' /srv/settlecore/.current.next; PATH=/tmp/test-bin:$PATH sh /rollback-current.sh; test $(readlink /srv/settlecore/current) = releases/' + $second + '; sh /rollback-current.sh; test $(readlink /srv/settlecore/current) = releases/' + $first + '; if sh /rollback-current.sh; then exit 1; fi; test $(readlink /srv/settlecore/current) = releases/' + $first
  Invoke-Docker run --rm --network none -v "${temp}/site:/srv/settlecore" -v "${rollback}:/rollback-current.sh:ro" alpine:3.20 sh -c $rollbackCommand
  $publishSecurityCommand = @'
set -eu
mkdir -p /tmp/test-bin
adduser -D -u 1001 deploy
install -d -o deploy -g deploy -m 0700 /srv/settlecore/incoming
install -d -o root -g root -m 0700 /srv/settlecore/staging
install -d -o root -g root -m 0755 /srv/settlecore/releases
su -s /bin/sh -c 'mkdir /srv/settlecore/incoming/test-release; printf published > /srv/settlecore/incoming/test-release/index.html' deploy
sh /publish-release.sh test-release
test "$(readlink /srv/settlecore/current)" = releases/test-release
test "$(stat -c %U /srv/settlecore/releases/test-release/index.html)" = root
test "$(stat -c %a /srv/settlecore/releases/test-release)" = 755
test "$(stat -c %a /srv/settlecore/releases/test-release/index.html)" = 644
if su -s /bin/sh -c 'printf TAMPERED >> /srv/settlecore/releases/test-release/index.html' deploy; then exit 1; fi
if su -s /bin/sh -c 'touch /srv/settlecore/releases/test-release/evil.html' deploy; then exit 1; fi
test ! -e /srv/settlecore/releases/test-release/evil.html
su -s /bin/sh -c 'mkdir /srv/settlecore/incoming/fd-release; printf orig > /srv/settlecore/incoming/fd-release/index.html; exec 3>> /srv/settlecore/incoming/fd-release/index.html; touch /tmp/fd-open; while test ! -e /tmp/fd-go; do sleep 0.1; done; printf FD_WRITE >&3; exec 3>&-' deploy &
writer=$!
while test ! -e /tmp/fd-open; do sleep 0.1; done
sh /publish-release.sh fd-release
touch /tmp/fd-go
wait "$writer"
test "$(cat /srv/settlecore/releases/fd-release/index.html)" = orig
printf '%s\n' "fd_handle_online_content=$(cat /srv/settlecore/releases/fd-release/index.html)"
rm -f /tmp/fd-open /tmp/fd-go
su -s /bin/sh -c 'mkdir /srv/settlecore/incoming/bad-release; ln -s /etc/passwd /srv/settlecore/incoming/bad-release/not-allowed' deploy
if sh /publish-release.sh bad-release; then exit 1; fi
test ! -e /srv/settlecore/staging/bad-release
cat >/tmp/test-bin/find <<'EOF'
#!/bin/sh
exit 66
EOF
chmod 0755 /tmp/test-bin/find
su -s /bin/sh -c 'mkdir /srv/settlecore/incoming/path-release; printf safe > /srv/settlecore/incoming/path-release/index.html' deploy
PATH=/tmp/test-bin:$PATH sh /publish-release.sh path-release
test -f /srv/settlecore/releases/path-release/index.html
rm -f /tmp/test-bin/find
mkdir -p /usr/local/bin
cat >/usr/local/bin/find <<'EOF'
#!/bin/sh
/usr/bin/find "$@"
result=$?
if test "$1" = /srv/settlecore/staging/window-release; then
  ln -s /etc/passwd /srv/settlecore/staging/window-release/after-validation
fi
exit "$result"
EOF
chmod 0755 /usr/local/bin/find
su -s /bin/sh -c 'mkdir /srv/settlecore/incoming/window-release; printf safe > /srv/settlecore/incoming/window-release/index.html' deploy
current_before=$(readlink /srv/settlecore/current)
if sh /publish-release.sh window-release; then exit 1; fi
test ! -e /srv/settlecore/releases/window-release
test "$(readlink /srv/settlecore/current)" = "$current_before"
rm -f /usr/local/bin/find
cat >/usr/local/bin/cp <<'EOF'
#!/bin/sh
if test "$1" = -R && test "$2" = --no-dereference; then
  shift 2
  /bin/cp -R -P "$@"
  result=$?
  if test "$result" -eq 0 && test "$2" = /srv/settlecore/staging/.new-postcopy-release/; then
    ln -s /etc/passwd /srv/settlecore/staging/.new-postcopy-release/after-copy
  fi
  exit "$result"
fi
exec /bin/cp "$@"
EOF
chmod 0755 /usr/local/bin/cp
su -s /bin/sh -c 'mkdir /srv/settlecore/incoming/postcopy-release; printf safe > /srv/settlecore/incoming/postcopy-release/index.html' deploy
if sh /publish-release.sh postcopy-release; then exit 1; fi
test ! -e /srv/settlecore/releases/postcopy-release
test "$(readlink /srv/settlecore/current)" = "$current_before"
rm -f /usr/local/bin/cp
'@
  $publishSecurityEncoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($publishSecurityCommand))
  Invoke-Docker run --rm --network none -v "${publish}:/publish-release.sh:ro" alpine:3.20 sh -c "printf '%s' $publishSecurityEncoded | base64 -d | sh"
  Write-Host 'AC-2 and AC-3 container verification passed.'
} finally {
  if (Test-Path -LiteralPath $dockerExe) { & $dockerExe rm -f settlecore-nginx-test 2>$null }
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
