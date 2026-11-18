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
DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" "$script_dir/admin-bootstrap.sh" --username 'Test Admin' >/dev/null
test "$(grep -c 'DROP ROLE IF EXISTS' "$root/sql")" -eq 1
test "$(grep -c 'CREATE ROLE' "$root/sql")" -eq 1
grep -q -- '--entrypoint /admininit' "$root/docker.log"
grep -q -- 'run --rm --no-deps -T --entrypoint /admininit' "$root/docker.log"
! grep -q 'printed-only-by-admininit' "$root/docker.log"
! grep -q 'bootstrap_password' "$root/docker.log"
! grep -Eq 'postgres://[^ ]*:[^ ]+@' "$root/docker.log"
echo 'admin bootstrap script: PASS'
