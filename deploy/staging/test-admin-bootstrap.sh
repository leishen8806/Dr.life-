#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname "$0")" && pwd)
root=$(mktemp -d); trap 'rm -rf "$root"' EXIT
fake_bin="$root/bin"; mkdir -p "$fake_bin"
cat > "$fake_bin/docker" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${FAKE_DOCKER_LOG:?}"
if [[ $* == *' exec '* ]]; then cat >> "${FAKE_SQL:?}"; else printf '%s\n' 'username=Test Admin' 'password=printed-only-by-admininit' 'totp=otpauth://totp/test'; fi
FAKE
chmod +x "$fake_bin/docker"
export FAKE_DOCKER_LOG="$root/docker.log" FAKE_SQL="$root/sql"
if DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" "$script_dir/admin-bootstrap.sh" --username 'Test Admin' >/dev/null 2>&1; then
  echo 'missing email was accepted' >&2
  exit 1
fi
DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" "$script_dir/admin-bootstrap.sh" --username 'Test Admin' --email 'owner@example.invalid' >/dev/null
test "$(grep -c 'DROP ROLE IF EXISTS' "$root/sql")" -eq 1
test "$(grep -c 'CREATE ROLE' "$root/sql")" -eq 1
grep -q -- '--entrypoint /admininit' "$root/docker.log"
grep -q -- 'run --rm --no-deps -T --entrypoint /admininit' "$root/docker.log"
! grep -q 'printed-only-by-admininit' "$root/docker.log"
! grep -q 'bootstrap_password' "$root/docker.log"
! grep -Eq 'postgres://[^ ]*:[^ ]+@' "$root/docker.log"

sudo_root=$(mktemp -d)
sudo_bin="$sudo_root/sudo"
cat > "$sudo_bin" <<'FAKE_SUDO'
#!/usr/bin/env bash
set -euo pipefail
preserved=${IAM_DATABASE_URL-}
if [[ ${1:-} != --preserve-env=IAM_DATABASE_URL ]]; then exit 1; fi
shift
unset IAM_DATABASE_URL
export IAM_DATABASE_URL="$preserved"
exec "$@"
FAKE_SUDO
chmod +x "$sudo_bin"
sudo_log="$root/sudo-docker.log"
sudo_sql="$root/sudo-sql"
sudo_marker="$root/sudo-iam-marker"
cat > "$fake_bin/docker-sudo" <<'FAKE_DOCKER_SUDO'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${SUDO_DOCKER_LOG:?}"
if [[ -n ${IAM_DATABASE_URL-} ]]; then printf '%s\n' present > "${SUDO_IAM_MARKER:?}"; else printf '%s\n' missing > "${SUDO_IAM_MARKER:?}"; fi
if [[ $* == *' exec '* ]]; then cat >> "${SUDO_SQL:?}"; else printf '%s\n' 'username=Test Admin' 'password=printed-only-by-admininit' 'totp=otpauth://totp/test'; fi
FAKE_DOCKER_SUDO
chmod +x "$fake_bin/docker-sudo"
DOCKER_BIN="$fake_bin/docker-sudo" SUDO_BIN="$sudo_bin" DOCKER_USE_SUDO=1 STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" SUDO_DOCKER_LOG="$sudo_log" SUDO_SQL="$sudo_sql" SUDO_IAM_MARKER="$sudo_marker" "$script_dir/admin-bootstrap.sh" --repair-email --username 'Test Admin' --email 'owner@example.invalid' >/dev/null
grep -qx present "$sudo_marker"
grep -q -- 'super-email' "$sudo_log"
! grep -q 'printed-only-by-admininit' "$sudo_log"
! grep -q 'postgres://[^ ]*:[^ ]+@' "$sudo_log"
echo 'admin bootstrap script: PASS'
