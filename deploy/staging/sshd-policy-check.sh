#!/usr/bin/env bash
set -euo pipefail

# Validate the effective SSH policy for every port reported by sshd. A
# Match LocalPort block makes lport mandatory in sshd -T -C.
validate_sshd_policy() {
  local ssh_user=$1
  local sshd_bin=${SSHD_BIN:-sshd}
  local -a ports=()
  local port effective expected

  while IFS= read -r port; do
    [[ -n $port ]] && ports+=("$port")
  done < <("$sshd_bin" -T | awk '$1 == "port" { print $2 }' | sort -nu)

  if ((${#ports[@]} == 0)); then
    printf '%s\n' 'sshd effective config did not report a listening port' >&2
    return 1
  fi

  for port in "${ports[@]}"; do
    effective=$("$sshd_bin" -T -C "user=$ssh_user,host=localhost,addr=127.0.0.1,laddr=127.0.0.1,lport=$port")
    for expected in 'passwordauthentication no' 'permitrootlogin no' 'kbdinteractiveauthentication no'; do
      if ! grep -Fxq "$expected" <<< "$effective"; then
        printf 'sshd effective config port %s missing: %s\n' "$port" "$expected" >&2
        return 1
      fi
    done
  done
}

restore_sshd_dropin() {
  local dropin=$1
  local previous=$2
  if [[ -f $previous ]]; then
    cp -p "$previous" "$dropin"
  else
    rm -f "$dropin"
  fi
}
