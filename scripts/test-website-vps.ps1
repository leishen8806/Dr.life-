$ErrorActionPreference = 'Stop'
$root = Resolve-Path (Join-Path $PSScriptRoot '..')
$dist = Join-Path $root 'apps/website/dist'
$siteConfig = Join-Path $root 'deploy/nginx/drlifeplus.com.conf'
$headers = Join-Path $root 'deploy/nginx/drlifeplus-security-headers.conf'
$rollback = Join-Path $root 'deploy/website/rollback-current.sh'
$publish = Join-Path $root 'deploy/website/publish-release.sh'
$releaseGuide = Join-Path $root 'docs/ops/website-release.md'
$dockerExe = 'C:\Program Files\Docker\Docker\resources\bin\docker.exe'
$opensslExe = (Get-Command openssl -ErrorAction Stop).Source
$temp = Join-Path ([IO.Path]::GetTempPath()) ("drlifeplus-nginx-" + [guid]::NewGuid())

function Invoke-Docker { & $dockerExe @args; if ($LASTEXITCODE -ne 0) { throw 'docker command failed' } }
function Invoke-Curl { param([string[]]$CurlArgs); $output = & curl.exe @CurlArgs; if ($LASTEXITCODE -ne 0) { throw 'curl command failed' }; $output }
function Assert-Contains { param([string]$Output, [string]$Expected); if ($Output -notmatch [regex]::Escape($Expected)) { throw "missing expected output: $Expected`n$Output" } }
function Assert-Equal { param([string]$Actual, [string]$Expected); if ($Actual -cne $Expected) { throw "expected: $Expected`nactual: $Actual" } }
function Assert-ConnectionClosed {
  param([string]$Url)
  $priorErrorAction = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  $status = & curl.exe -sk -o NUL -w '%{http_code}' -H 'Host: unconfigured.invalid' $Url 2>$null
  $exitCode = $LASTEXITCODE
  $ErrorActionPreference = $priorErrorAction
  if ($exitCode -eq 0 -or $status -ne '000') { throw "expected nginx 444 connection close: exit=$exitCode status=$status url=$Url" }
}
function Get-StringSha256 { param([string]$Value); ([BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($Value)))).Replace('-', '').ToLowerInvariant() }
function Test-GuideSudoersCase {
  param([string]$GuideScript, [string]$SudoList)
  $encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($SudoList))
  $fixtureCommand = "printf '%s' $encoded | base64 -d"
  $caseScript = $GuideScript.Replace('sudo -l -U drlifeplus-deploy', $fixtureCommand)
  $caseEncoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($caseScript))
  $priorErrorAction = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  & $dockerExe run --rm --network none alpine:3.20 sh -c "printf '%s' $caseEncoded | base64 -d | sh" 1>$null 2>$null
  $ErrorActionPreference = $priorErrorAction
  return $LASTEXITCODE -eq 0
}

try {
  Assert-Equal (Get-Content -LiteralPath $publish -TotalCount 1) '#!/bin/sh'
  Assert-Equal (Get-Content -LiteralPath $rollback -TotalCount 1) '#!/bin/sh'
  $guideText = Get-Content -Raw -Encoding UTF8 -LiteralPath $releaseGuide
  $guideMatch = [regex]::Match($guideText, '(?s)# sudoers-check-start\r?\n(.*?)# sudoers-check-end')
  if (-not $guideMatch.Success) { throw 'missing sudoers-check markers in release guide' }
  $guideScript = $guideMatch.Groups[1].Value.Trim()
  $stepNumbers = [regex]::Matches($guideText, '(?m)^### [^\r\n]* ([1-6])\D') | ForEach-Object { $_.Groups[1].Value }
  Assert-Equal ($stepNumbers -join ',') '1,2,3,4,5,6'
  Assert-Equal (($guideScript -split "`n")[0].Trim()) '('
  Assert-Equal (($guideScript -split "`n")[-1].Trim()) ')'
  $legacyGuideScript = @'
sudo -l -U drlifeplus-deploy | tr -d '\\' | tr ',' '\n' | grep -E '^[[:space:]]*secure_path=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin(:/snap/bin)?[[:space:]]*$'
'@
  $securePath = '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'
  $sudoCases = @(
    @{ Name = 'debian'; Output = "Matching Defaults entries for drlifeplus-deploy on host:`n    env_reset, mail_badpass, secure_path=$($securePath -replace ':', '\:'), use_pty"; Expected = $true }
    @{ Name = 'ubuntu-snap'; Output = "Matching Defaults entries for drlifeplus-deploy on host:`n    env_reset, secure_path=$($securePath -replace ':', '\:')\:/snap/bin, use_pty"; Expected = $true }
    @{ Name = 'commented'; Output = "Matching Defaults entries for drlifeplus-deploy on host:`n    env_reset, mail_badpass, use_pty"; Expected = $false }
    @{ Name = 'disabled'; Output = "Matching Defaults entries for drlifeplus-deploy on host:`n    env_reset, secure_path=$($securePath -replace ':', '\:'), !secure_path, use_pty"; Expected = $false }
    @{ Name = 'env-keep'; Output = "Matching Defaults entries for drlifeplus-deploy on host:`n    env_reset, secure_path=$($securePath -replace ':', '\:'), env_keep+=PATH, use_pty"; Expected = $true }
    @{ Name = 'polluted'; Output = "Matching Defaults entries for drlifeplus-deploy on host:`n    env_reset, secure_path=/tmp/evil\:/usr/local/sbin\:/usr/local/bin\:/usr/sbin\:/usr/bin\:/sbin\:/bin, use_pty"; Expected = $false }
    @{ Name = 'no-entry'; Output = 'User drlifeplus-deploy is not allowed to run sudo on host.'; Expected = $true }
    @{ Name = 'sudo-rs'; Output = "User drlifeplus-deploy may run the following commands on host:`n    (root) NOPASSWD: /usr/local/sbin/drlifeplus-website-publish *"; Expected = $true }
  )
  Assert-Equal (Get-StringSha256 $legacyGuideScript) '8e69522a08cedaae3e31f6a0b440fcd3e33b8fc1ea7fb5bf86fe2c6360f335c1'
  $polluted = $sudoCases | Where-Object Name -eq 'polluted' | Select-Object -First 1
  Assert-Equal ([string]($polluted.Output -split "`n").Count) '2'
  $debian = $sudoCases | Where-Object Name -eq 'debian' | Select-Object -First 1
  Assert-Equal ([string]($debian.Output -match '\\\\:')) 'False'
  $disabled = $sudoCases | Where-Object Name -eq 'disabled' | Select-Object -First 1
  $noEntry = $sudoCases | Where-Object Name -eq 'no-entry' | Select-Object -First 1
  $sudoRs = $sudoCases | Where-Object Name -eq 'sudo-rs' | Select-Object -First 1
  Assert-Equal ([string](Test-GuideSudoersCase $legacyGuideScript $disabled.Output)) 'True'
  Assert-Equal ([string](Test-GuideSudoersCase $legacyGuideScript $noEntry.Output)) 'False'
  Assert-Equal ([string](Test-GuideSudoersCase $legacyGuideScript $sudoRs.Output)) 'False'
  foreach ($case in $sudoCases) {
    Assert-Equal ([string](Test-GuideSudoersCase $guideScript $case.Output)) ([string]$case.Expected)
  }
  Write-Host 'R1 legacy disabled/no-entry/sudo-rs mutations matched the 78c502b behavior.'
  Write-Host 'R1 guide-command real sudoers fixture verification passed.'
  $first = '20261104T000001Z-first'; $second = '20261104T000002Z-second'; $third = '20261104T000003Z-third'
  New-Item -ItemType Directory -Path "$temp/site/releases/$first", "$temp/site/releases/$second", "$temp/site/releases/$third", "$temp/snippets", "$temp/tls" | Out-Null
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$first/"
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$second/"
  Copy-Item -Recurse "$dist/*" "$temp/site/releases/$third/"
  Set-Content -NoNewline -LiteralPath "$temp/site/releases/$first/version.txt" -Value 'first'
  Set-Content -NoNewline -LiteralPath "$temp/site/releases/$second/version.txt" -Value 'second'
  Set-Content -NoNewline -LiteralPath "$temp/site/releases/$third/version.txt" -Value 'third'
  Set-Content -NoNewline -LiteralPath "$temp/site/escape.txt" -Value 'outside-root'
  Invoke-Docker run --rm --network none -v "${temp}:/work" alpine:3.20 ln -s ../../escape.txt "/work/site/releases/$first/escape.txt"
  Invoke-Docker run --rm --network none -v "${temp}:/work" alpine:3.20 ln -s "releases/$first" /work/site/current
  Copy-Item $headers "$temp/snippets/drlifeplus-security-headers.conf"
  Push-Location "$temp/tls"
  try { & $opensslExe req -x509 -nodes -newkey rsa:2048 -days 1 -subj '/CN=drlifeplus.test' -keyout drlifeplus-origin.key -out drlifeplus-origin.crt 2>$null } finally { Pop-Location }
  if ($LASTEXITCODE -ne 0) { throw 'temporary certificate generation failed' }
  (Get-Content -Raw $siteConfig).Replace('listen 80 default_server;', 'listen 8080 default_server;').Replace('listen [::]:80 default_server;', 'listen [::]:8080 default_server;').Replace('listen 443 ssl default_server;', 'listen 8443 ssl default_server;').Replace('listen [::]:443 ssl default_server;', 'listen [::]:8443 ssl default_server;').Replace('listen 80;', 'listen 8080;').Replace('listen [::]:80;', 'listen [::]:8080;').Replace('listen 443 ssl;', 'listen 8443 ssl;').Replace('listen [::]:443 ssl;', 'listen [::]:8443 ssl;').Replace('/etc/nginx/tls/', '/work/tls/').Replace('/srv/drlifeplus/current', '/work/site/current').Replace('/etc/nginx/snippets/', '/work/snippets/') | Set-Content -NoNewline -LiteralPath "$temp/drlifeplus.com.conf"
  @'
events {}
http { include /etc/nginx/mime.types; include /work/drlifeplus.com.conf; }
'@ | Set-Content -NoNewline -LiteralPath "$temp/nginx.conf"
  Invoke-Docker run --rm --network none -v "${temp}:/work:ro" nginx:1.27-alpine nginx -c /work/nginx.conf -t
  Write-Host 'nginx -t with a temporary self-signed certificate passed.'
  Invoke-Docker run --rm -d --name drlifeplus-nginx-test -p 18080:8080 -p 18443:8443 -v "${temp}:/work:ro" nginx:1.27-alpine nginx -c /work/nginx.conf -g 'daemon off;'
  Start-Sleep -Seconds 1
  Assert-ConnectionClosed 'http://127.0.0.1:18080/'
  Assert-ConnectionClosed 'https://127.0.0.1:18443/'
  Write-Host 'HTTP and HTTPS default servers returned nginx 444 connection closes.'
  $homeResponse = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', 'https://127.0.0.1:18443/')
  Write-Host $homeResponse
  Assert-Contains $homeResponse 'HTTP/1.1 200 OK'; Assert-Contains $homeResponse 'Content-Security-Policy:'; Assert-Contains $homeResponse 'Cache-Control: no-cache'
  $gzip = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', '-H', 'Accept-Encoding: gzip', 'https://127.0.0.1:18443/')
  Write-Host $gzip
  Assert-Contains $gzip 'Content-Encoding: gzip'
  $asset = (Get-ChildItem "$temp/site/releases/$first/_astro" -File | Select-Object -First 1).Name
  $assetHead = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', "https://127.0.0.1:18443/_astro/$asset")
  Write-Host $assetHead
  Assert-Contains $assetHead 'Cache-Control: public, max-age=31536000, immutable'; Assert-Contains $assetHead 'Content-Security-Policy:'
  $font = (Get-ChildItem "$temp/site/releases/$first/fonts" -Filter '*.woff2' -File | Select-Object -First 1).Name
  $fontHead = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', "https://127.0.0.1:18443/fonts/$font")
  Write-Host $fontHead
  Assert-Contains $fontHead 'Content-Security-Policy:'
  $missing = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', 'https://127.0.0.1:18443/_astro/missing.js')
  Write-Host $missing
  Assert-Contains $missing '404 Not Found'; Assert-Contains $missing 'Content-Security-Policy:'
  $redirect = Invoke-Curl -CurlArgs @('-sI', '-H', 'Host: www.drlifeplus.com', 'http://127.0.0.1:18080/terms?lang=km')
  Write-Host $redirect
  Assert-Contains $redirect '301 Moved Permanently'; Assert-Contains $redirect 'Location: https://drlifeplus.com/terms?lang=km'
  $privacy = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', 'https://127.0.0.1:18443/privacy')
  Write-Host $privacy
  Assert-Contains $privacy '301 Moved Permanently'; Assert-Contains $privacy 'Location: /privacy/'
  $directory = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', 'https://127.0.0.1:18443/fonts/')
  Write-Host $directory
  Assert-Contains $directory '404 Not Found'
  $hidden = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', 'https://127.0.0.1:18443/.env')
  Write-Host $hidden
  Assert-Contains $hidden '403 Forbidden'
  $symlink = Invoke-Curl -CurlArgs @('-skI', '-H', 'Host: drlifeplus.com', 'https://127.0.0.1:18443/escape.txt')
  Write-Host $symlink
  Assert-Contains $symlink '404 Not Found'
  $rollbackCommand = 'set -eu; mkdir -p /tmp/test-bin; printf ''#!/bin/sh\nexit 66\n'' >/tmp/test-bin/find; chmod 0755 /tmp/test-bin/find; rm -f /srv/drlifeplus/current; ln -s releases/' + $third + ' /srv/drlifeplus/current; ln -s releases/' + $first + ' /srv/drlifeplus/.current.next; PATH=/tmp/test-bin:$PATH sh /rollback-current.sh; test $(readlink /srv/drlifeplus/current) = releases/' + $second + '; sh /rollback-current.sh; test $(readlink /srv/drlifeplus/current) = releases/' + $first + '; if sh /rollback-current.sh; then exit 1; fi; test $(readlink /srv/drlifeplus/current) = releases/' + $first
  Invoke-Docker run --rm --network none -v "${temp}/site:/srv/drlifeplus" -v "${rollback}:/rollback-current.sh:ro" alpine:3.20 sh -c $rollbackCommand
  $publishSecurityCommand = @'
set -eu
mkdir -p /tmp/test-bin
adduser -D -u 1001 deploy
install -d -o deploy -g deploy -m 0700 /srv/drlifeplus/incoming
install -d -o root -g root -m 0700 /srv/drlifeplus/staging
install -d -o root -g root -m 0755 /srv/drlifeplus/releases
mkdir /srv/drlifeplus/.current.next
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/next-directory-release; printf published > /srv/drlifeplus/incoming/next-directory-release/index.html' deploy
if ! sh /publish-release.sh next-directory-release; then
  if test -e /srv/drlifeplus/releases/next-directory-release; then
    printf '%s\n' 'publish left an orphan release after a directory blocked .current.next cleanup' >&2
  fi
  exit 1
fi
test "$(readlink /srv/drlifeplus/current)" = releases/next-directory-release
printf stale > /srv/drlifeplus/.current.next
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/next-file-release; printf published > /srv/drlifeplus/incoming/next-file-release/index.html' deploy
sh /publish-release.sh next-file-release
test "$(readlink /srv/drlifeplus/current)" = releases/next-file-release
ln -s missing-target /srv/drlifeplus/.current.next
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/next-dangling-release; printf published > /srv/drlifeplus/incoming/next-dangling-release/index.html' deploy
sh /publish-release.sh next-dangling-release
test "$(readlink /srv/drlifeplus/current)" = releases/next-dangling-release
if find /srv/drlifeplus/releases ! -type d ! -type f -print -quit | grep -q .; then exit 1; fi
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/test-release; printf published > /srv/drlifeplus/incoming/test-release/index.html' deploy
sh /publish-release.sh test-release
test "$(readlink /srv/drlifeplus/current)" = releases/test-release
test "$(stat -c %U /srv/drlifeplus/releases/test-release/index.html)" = root
test "$(stat -c %h /srv/drlifeplus/releases/test-release/index.html)" = 1
test "$(stat -c %a /srv/drlifeplus/releases/test-release)" = 755
test "$(stat -c %a /srv/drlifeplus/releases/test-release/index.html)" = 644
if su -s /bin/sh -c 'printf TAMPERED >> /srv/drlifeplus/releases/test-release/index.html' deploy; then exit 1; fi
if su -s /bin/sh -c 'touch /srv/drlifeplus/releases/test-release/evil.html' deploy; then exit 1; fi
test ! -e /srv/drlifeplus/releases/test-release/evil.html
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/fd-release; printf orig > /srv/drlifeplus/incoming/fd-release/index.html; exec 3>> /srv/drlifeplus/incoming/fd-release/index.html; touch /tmp/fd-open; while test ! -e /tmp/fd-go; do sleep 0.1; done; printf FD_WRITE >&3; exec 3>&-' deploy &
writer=$!
while test ! -e /tmp/fd-open; do sleep 0.1; done
sh /publish-release.sh fd-release
touch /tmp/fd-go
wait "$writer"
test "$(cat /srv/drlifeplus/releases/fd-release/index.html)" = orig
printf '%s\n' "fd_handle_online_content=$(cat /srv/drlifeplus/releases/fd-release/index.html)"
rm -f /tmp/fd-open /tmp/fd-go
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/bad-release; ln -s /etc/passwd /srv/drlifeplus/incoming/bad-release/not-allowed' deploy
if sh /publish-release.sh bad-release; then exit 1; fi
test ! -e /srv/drlifeplus/staging/bad-release
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/hardlink-release; printf same-inode > /srv/drlifeplus/incoming/hardlink-release/index.html; ln /srv/drlifeplus/incoming/hardlink-release/index.html /srv/drlifeplus/incoming/hardlink-release/copy.html' deploy
if sh /publish-release.sh hardlink-release; then printf '%s\n' 'publish accepted a multiply linked file' >&2; exit 1; fi
test ! -e /srv/drlifeplus/releases/hardlink-release
test ! -e /srv/drlifeplus/staging/hardlink-release
cat >/tmp/test-bin/find <<'EOF'
#!/bin/sh
exit 66
EOF
chmod 0755 /tmp/test-bin/find
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/path-release; printf safe > /srv/drlifeplus/incoming/path-release/index.html' deploy
PATH=/tmp/test-bin:$PATH sh /publish-release.sh path-release
test -f /srv/drlifeplus/releases/path-release/index.html
printf '%s\n' 'path_hijack_fixed_path=passed'
rm -f /tmp/test-bin/find
mkdir -p /usr/local/bin
cat >/usr/local/bin/find <<'EOF'
#!/bin/sh
/usr/bin/find "$@"
result=$?
if test "$1" = /srv/drlifeplus/staging/window-release && ! test -e /srv/drlifeplus/staging/window-release/after-validation; then
  ln -s /etc/passwd /srv/drlifeplus/staging/window-release/after-validation
fi
exit "$result"
EOF
chmod 0755 /usr/local/bin/find
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/window-release; printf safe > /srv/drlifeplus/incoming/window-release/index.html' deploy
current_before=$(readlink /srv/drlifeplus/current)
if sh /publish-release.sh window-release; then exit 1; fi
test ! -e /srv/drlifeplus/releases/window-release
test "$(readlink /srv/drlifeplus/current)" = "$current_before"
printf '%s\n' 'validation_window_injection=blocked'
rm -f /usr/local/bin/find
cat >/usr/local/bin/cp <<'EOF'
#!/bin/sh
if test "$1" = -R && test "$2" = --no-dereference; then
  shift 2
  /bin/cp -R -P "$@"
  result=$?
  if test "$result" -eq 0 && test "$2" = /srv/drlifeplus/staging/.new-postcopy-release/; then
    ln -s /etc/passwd /srv/drlifeplus/staging/.new-postcopy-release/after-copy
  fi
  exit "$result"
fi
exec /bin/cp "$@"
EOF
chmod 0755 /usr/local/bin/cp
su -s /bin/sh -c 'mkdir /srv/drlifeplus/incoming/postcopy-release; printf safe > /srv/drlifeplus/incoming/postcopy-release/index.html' deploy
if sh /publish-release.sh postcopy-release; then exit 1; fi
test ! -e /srv/drlifeplus/releases/postcopy-release
test "$(readlink /srv/drlifeplus/current)" = "$current_before"
printf '%s\n' 'postcopy_injection=blocked'
rm -f /usr/local/bin/cp
'@
  $publishSecurityEncoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($publishSecurityCommand))
  Invoke-Docker run --rm --network none -v "${publish}:/publish-release.sh:ro" alpine:3.20 sh -c "printf '%s' $publishSecurityEncoded | base64 -d | sh"
  Write-Host 'AC-2 and AC-3 container verification passed.'
} finally {
  if (Test-Path -LiteralPath $dockerExe) { & $dockerExe rm -f drlifeplus-nginx-test 2>$null }
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
