#!/usr/bin/env bash
# shellcheck source=deploy/staging/sshd-policy-check.sh
set -euo pipefail

script_dir=$(cd -- "$(dirname "$0")" && pwd)
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
fake_bin="$root/bin"
mkdir -p "$fake_bin"

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
  printf '%s\n' 'port 22' 'port 2222'
fi
FAKE_SSHD
chmod +x "$fake_bin/sshd"

source "$script_dir/sshd-policy-check.sh"
export PATH="$fake_bin:$PATH" SSHD_BIN=sshd FAKE_SSHD_LOG="$root/sshd.log"
validate_sshd_policy deploy
test "$(wc -l < "$FAKE_SSHD_LOG")" -eq 2
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=22' "$FAKE_SSHD_LOG"
grep -Fxq 'user=deploy,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=2222' "$FAKE_SSHD_LOG"

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
