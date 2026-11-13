#!/usr/bin/env bash
set -euo pipefail

mode=dry-run
ssh_source_ip=
for argument in "$@"; do
  case "$argument" in
    --dry-run) mode=dry-run ;;
    --apply) mode=apply ;;
    --ssh-source-ip=*) ssh_source_ip=${argument#*=} ;;
    *) printf 'usage: %s [--dry-run|--apply] [--ssh-source-ip=CIDR]\n' "$0" >&2; exit 64 ;;
  esac
done

cert_path=${STAGING_ORIGIN_CERT_PATH:-/srv/drlifeplus-staging/certs/cloudflare-origin.crt}
key_path=${STAGING_ORIGIN_KEY_PATH:-/srv/drlifeplus-staging/certs/cloudflare-origin.key}
sshd_dropin=/etc/ssh/sshd_config.d/99-drlifeplus-staging.conf
plan() { printf '[%s] %s\n' "$mode" "$*"; }

if [[ $mode == dry-run ]]; then
  plan "fetch Cloudflare ranges from https://www.cloudflare.com/ips-v4 and /ips-v6"
  plan "allow Cloudflare ranges to TCP 80/443 and keep TCP 22 open"
  [[ -n $ssh_source_ip ]] && plan "restrict TCP 22 to owner-provided source CIDR: $ssh_source_ip"
  plan "write $sshd_dropin, validate sshd, enable fail2ban, and check mode-600 Origin files"
  exit 0
fi
if [[ $EUID -ne 0 ]]; then printf '%s\n' '--apply must run as root' >&2; exit 1; fi
if ! compgen -G "${HOME}/.ssh/authorized_keys" >/dev/null; then
  printf '%s\n' 'no authorized SSH public key; refusing to disable password login' >&2
  exit 1
fi
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
curl --fail --silent --show-error --location https://www.cloudflare.com/ips-v4 > "$tmpdir/ips-v4"
curl --fail --silent --show-error --location https://www.cloudflare.com/ips-v6 > "$tmpdir/ips-v6"
install -d -m 755 /etc/ssh/sshd_config.d
if [[ -f $sshd_dropin ]]; then cp -p "$sshd_dropin" "$sshd_dropin.bak"; fi
printf '%s\n' 'PasswordAuthentication no' 'PermitRootLogin no' 'KbdInteractiveAuthentication no' > "$sshd_dropin"
if ! sshd -t; then
  [[ -f "$sshd_dropin.bak" ]] && mv "$sshd_dropin.bak" "$sshd_dropin" || true
  printf '%s\n' 'sshd -t failed; daemon was not restarted' >&2
  exit 1
fi
chmod 600 "$sshd_dropin"
if [[ ! -f $cert_path || ! -f $key_path ]]; then printf '%s\n' 'Origin certificate and key must exist before --apply' >&2; exit 1; fi
cert_mode=$(stat -c '%a' "$cert_path" 2>/dev/null || stat -f '%Lp' "$cert_path")
key_mode=$(stat -c '%a' "$key_path" 2>/dev/null || stat -f '%Lp' "$key_path")
if [[ $cert_mode != 600 || $key_mode != 600 ]]; then printf '%s\n' 'Origin certificate and key must both have mode 600' >&2; exit 1; fi
apt-get update
apt-get install -y fail2ban ufw
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
for ranges in "$tmpdir/ips-v4" "$tmpdir/ips-v6"; do
  while IFS= read -r cidr; do
    [[ -n $cidr ]] || continue
    ufw allow from "$cidr" to any port 80 proto tcp
    ufw allow from "$cidr" to any port 443 proto tcp
  done < "$ranges"
done
if [[ -n $ssh_source_ip ]]; then ufw allow from "$ssh_source_ip" to any port 22 proto tcp; else ufw allow 22/tcp; fi
ufw --force enable
systemctl enable --now fail2ban
systemctl reload sshd
printf '%s\n' 'Applied. Keep the current SSH connection open until a second key-only login succeeds.'
