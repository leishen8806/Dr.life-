#!/usr/bin/env bash
# shellcheck source=deploy/staging/sshd-policy-check.sh
set -euo pipefail

script_dir=$(cd -- "$(dirname "$0")" && pwd)
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
fake_bin="$root/bin"
mkdir -p "$fake_bin"

cat > "$fake_bin/ss" <<'FAKE_SS'
#!/usr/bin/env bash
if [[ ${FAKE_SS_MODE:-} == listen ]]; then
  printf '%s\n' 'LISTEN 0 128 0.0.0.0:2200 0.0.0.0:* users:(("sshd",pid=1,fd=3))'
fi
FAKE_SS
chmod +x "$fake_bin/ss"

cat > "$fake_bin/sshd" <<'FAKE_SSHD'
#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == -t ]]; then exit 0; fi
if [[ $1 != -T ]]; then exit 64; fi
if [[ ${2:-} == -C ]]; then
  context=$3
  printf '%s\n' "$context" >> "${FAKE_SSHD_LOG:?}"
  port=${context##*lport=}
  if [[ ${FAKE_SSHD_FAIL_PORT:-} == "$port" ]]; then
    printf '%s\n' 'passwordauthentication yes' 'permitrootlogin no' 'kbdinteractiveauthentication no'
  else
    printf '%s\n' 'passwordauthentication no' 'permitrootlogin no' 'kbdinteractiveauthentication no'
  fi
else
  if [[ ${FAKE_SSHD_NO_PORTS:-} == 1 ]]; then
    exit 0
  fi
  printf '%s\n' 'port 22' 'port 2222'
fi
FAKE_SSHD
chmod +x "$fake_bin/sshd"

source "$script_dir/sshd-policy-check.sh"
export PATH="$fake_bin:$PATH" SSHD_BIN=sshd FAKE_SSHD_LOG="$root/sshd.log"
export SS_BIN=ss
validate_sshd_policy deploy
test "$(wc -l < "$FAKE_SSHD_LOG")" -eq 2
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=22' "$FAKE_SSHD_LOG"
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=2222' "$FAKE_SSHD_LOG"

: > "$FAKE_SSHD_LOG"
export FAKE_SSHD_NO_PORTS=1
validate_sshd_policy deploy
test "$(wc -l < "$FAKE_SSHD_LOG")" -eq 1
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=22' "$FAKE_SSHD_LOG"
unset FAKE_SSHD_NO_PORTS

config="$root/sshd_config"
cat > "$config" <<'CONFIG'
port 2201
ListenAddress 127.0.0.1:2202
Match LocalPort 2203,2204
CONFIG
: > "$FAKE_SSHD_LOG"
export SSHD_CONFIG="$config"
export FAKE_SSHD_NO_PORTS=1
export FAKE_SSHD_FAIL_PORT=2203
if validate_sshd_policy deploy; then
  printf '%s\n' 'expected config-derived LocalPort policy to fail' >&2
  exit 1
fi
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=2201' "$FAKE_SSHD_LOG"
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=2202' "$FAKE_SSHD_LOG"
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=2203' "$FAKE_SSHD_LOG"
unset FAKE_SSHD_FAIL_PORT
unset FAKE_SSHD_NO_PORTS

dropin="$root/00-drlifeplus-staging.conf"
previous="$root/previous.conf"
printf '%s\n' 'old-policy' > "$previous"
printf '%s\n' 'new-policy' > "$dropin"
: > "$FAKE_SSHD_LOG"
export FAKE_SSHD_FAIL_PORT=2222
if validate_sshd_policy deploy; then
  printf '%s\n' 'expected multi-port validation to fail' >&2
  exit 1
fi
restore_sshd_dropin "$dropin" "$previous"
grep -Fxq 'old-policy' "$dropin"

echo 'sshd policy check: PASS'
