#!/usr/bin/env bash
set -euo pipefail

# Validate the effective SSH policy for every port reported by sshd. A
# Match LocalPort block makes lport mandatory in sshd -T -C.
collect_config_ports() {
  # shellcheck disable=SC2094
  local file=$1 line directive value ports_spec include
  [[ -r $file ]] || return 0
  while IFS= read -r line || [[ -n $line ]]; do
    line=${line%%#*}
    read -r directive value ports_spec _ <<< "$line"
    directive=$(printf '%s' "$directive" | tr '[:upper:]' '[:lower:]')
    case "$directive" in
      include)
        for include in $value; do
          for file in $include; do collect_config_ports "$file"; done
        done
        ;;
      port) printf '%s\n' "$value" ;;
      listenaddress)
        [[ $value =~ \]:([0-9]+)$ ]] && printf '%s\n' "${BASH_REMATCH[1]}"
        [[ $value =~ :([0-9]+)$ ]] && printf '%s\n' "${BASH_REMATCH[1]}"
        ;;
      match)
        if [[ $(printf '%s' "$value" | tr '[:upper:]' '[:lower:]') == localport ]]; then
          printf '%s\n' "$ports_spec" | tr ',' '\n'
        fi
        ;;
    esac
  done < "$file"
}

validate_sshd_policy() {
  local ssh_user=$1
  local sshd_bin=${SSHD_BIN:-sshd}
  local ss_bin=${SS_BIN:-ss}
  local config_file=${SSHD_CONFIG:-/etc/ssh/sshd_config}
  local -a ports=()
  local port effective expected port_output

  # The kernel's listener table is authoritative for ports that are actually
  # reachable. Include sshd -T output as a second source before parsing text
  # config, because Match LocalPort can make the context-free query fail.
  while IFS= read -r port; do
    [[ $port =~ ^[0-9]+$ ]] && ports+=("$port")
  done < <("$ss_bin" -ltnpH 2>/dev/null | awk '/sshd/ { print $4 }' | sed -E 's/.*:([0-9]+)$/\1/' | sort -nu)

  # `sshd -T` normally provides the effective Port lines. Some valid configs
  # containing Match LocalPort refuse the context-free query; in that case
  # use explicit Port directives and OpenSSH's default of 22 as the safe
  # fallback, then still validate every discovered port with lport supplied.
  port_output=$("$sshd_bin" -T 2>/dev/null || true)
  while IFS= read -r port; do
    [[ $port =~ ^[0-9]+$ ]] && ports+=("$port")
  done < <(printf '%s\n' "$port_output" | awk '$1 == "port" { print $2 }' | sort -nu)

  if ((${#ports[@]} == 0)); then
    while IFS= read -r port; do
      [[ $port =~ ^[0-9]+$ ]] && ports+=("$port")
    done < <(collect_config_ports "$config_file" | sort -nu)
  fi
  if ((${#ports[@]} == 0)); then
    ports+=(22)
    printf '%s\n' 'warning: unable to confirm listening SSH port; validating default 22' >&2
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
