#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname "$0")" && pwd)
root=$(mktemp -d); trap 'rm -rf "$root"' EXIT
fake_bin="$root/bin"; mkdir -p "$fake_bin"
cat > "$fake_bin/docker" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${FAKE_DOCKER_LOG:?}"
if [[ $* == *' exec '* ]]; then
  query=$(cat)
  if [[ $query == *'SELECT public_id FROM iam.admin_user'* ]]; then
    printf '%s\n' "${FAKE_ACTOR_ID-01ACTORPUBLICID}"
  else
    printf '%s\n' "$query" >> "${FAKE_SQL:?}"
  fi
else
  cat > "${FAKE_STDIN:?}"
  printf '%s\n' 'username=Test Admin' 'password=printed-only-by-admininit' 'totp=otpauth://totp/test'
fi
FAKE
chmod +x "$fake_bin/docker"
export FAKE_DOCKER_LOG="$root/docker.log" FAKE_SQL="$root/sql"
export FAKE_STDIN="$root/stdin"
DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" "$script_dir/admin-bootstrap.sh" --username 'Test Admin' >/dev/null
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
if [[ $* == *' exec '* ]]; then
  query=$(cat)
  if [[ $query == *'SELECT public_id FROM iam.admin_user'* ]]; then
    printf '%s\n' "${SUDO_ACTOR_ID-01ACTORPUBLICID}"
  else
    printf '%s\n' "$query" >> "${SUDO_SQL:?}"
  fi
else
  cat > "${SUDO_STDIN:?}"
  printf '%s\n' 'username=Test Admin' 'password=printed-only-by-admininit' 'totp=otpauth://totp/test'
fi
FAKE_DOCKER_SUDO
chmod +x "$fake_bin/docker-sudo"
DOCKER_BIN="$fake_bin/docker-sudo" SUDO_BIN="$sudo_bin" DOCKER_USE_SUDO=1 STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" SUDO_DOCKER_LOG="$sudo_log" SUDO_SQL="$sudo_sql" SUDO_IAM_MARKER="$sudo_marker" SUDO_STDIN="$root/sudo-stdin" "$script_dir/admin-bootstrap.sh" --username 'Test Admin' >/dev/null
grep -qx present "$sudo_marker"
! grep -q 'printed-only-by-admininit' "$sudo_log"
! grep -q 'postgres://[^ ]*:[^ ]+@' "$sudo_log"

bot_output="$root/bot-output"
bot_error="$root/bot-error"
bot_token='123456:token-must-stay-on-stdin'
FAKE_ACTOR_ID='01ACTORPUBLICID' FAKE_STDIN="$root/bot-stdin" \
  DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" \
  FAKE_DOCKER_LOG="$root/bot-docker.log" FAKE_SQL="$root/bot-sql" \
  "$script_dir/admin-bootstrap.sh" --bot buyer --bot-username 'BuyerBot' --actor-username 'Test Admin' \
  >"$bot_output" 2>"$bot_error" <<EOF
$bot_token
EOF
grep -qx "$bot_token" "$root/bot-stdin"
grep -q -- '--end buyer --username BuyerBot --actor 01ACTORPUBLICID' "$root/bot-docker.log"
! grep -R -q -- "$bot_token" "$bot_output" "$bot_error" "$root/bot-docker.log" "$root/bot-sql"
grep -q -- 'DROP ROLE IF EXISTS' "$root/bot-sql"

merchant_token='123456:merchant-token-must-stay-on-stdin'
FAKE_ACTOR_ID='01ACTORPUBLICID' FAKE_STDIN="$root/merchant-stdin" \
  DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" \
  FAKE_DOCKER_LOG="$root/merchant-docker.log" FAKE_SQL="$root/merchant-sql" \
  "$script_dir/admin-bootstrap.sh" --bot merchant --bot-username 'MerchantBot' --actor-username 'Test Admin' \
  >/dev/null 2>"$root/merchant-error" <<EOF
$merchant_token
EOF
grep -qx "$merchant_token" "$root/merchant-stdin"
grep -q -- '--end merchant --username MerchantBot --actor 01ACTORPUBLICID' "$root/merchant-docker.log"
! grep -R -q -- "$merchant_token" "$root/merchant-error" "$root/merchant-docker.log" "$root/merchant-sql"
grep -q -- 'DROP ROLE IF EXISTS' "$root/merchant-sql"

crlf_token='123456:crlf-token-must-stay-on-stdin'
printf '%s\r\n' "$crlf_token" | \
  FAKE_ACTOR_ID='01ACTORPUBLICID' FAKE_STDIN="$root/crlf-stdin" \
  DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" \
  FAKE_DOCKER_LOG="$root/crlf-docker.log" FAKE_SQL="$root/crlf-sql" \
  "$script_dir/admin-bootstrap.sh" --bot buyer --bot-username 'BuyerBot' --actor-username 'Test Admin' \
  >/dev/null 2>"$root/crlf-error"
grep -qx "$crlf_token" "$root/crlf-stdin"

if FAKE_ACTOR_ID=$'01ACTORPUBLICID\n02ACTORPUBLICID' FAKE_STDIN="$root/duplicate-stdin" \
  DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" \
  FAKE_DOCKER_LOG="$root/duplicate-docker.log" FAKE_SQL="$root/duplicate-sql" \
  "$script_dir/admin-bootstrap.sh" --bot buyer --bot-username 'BuyerBot' --actor-username 'Duplicate Admin' \
  >/dev/null 2>"$root/duplicate-error" <<'EOF'
123456:token-for-duplicate-actor
EOF
then
  exit 1
fi
grep -q -- 'exactly one active super administrator was required' "$root/duplicate-error"

if "$script_dir/admin-bootstrap.sh" --bot >/dev/null 2>"$root/missing-args"; then exit 1; fi
if "$script_dir/admin-bootstrap.sh" --bot driver --bot-username 'BadBot' --actor-username 'Test Admin' </dev/null >/dev/null 2>"$root/bad-end"; then exit 1; fi
grep -q -- 'usage:' "$root/missing-args"
grep -q -- 'usage:' "$root/bad-end"
if "$script_dir/admin-bootstrap.sh" --bot buyer --bot-username 'BuyerBot' --actor-username 'Missing Admin' </dev/null >/dev/null 2>"$root/missing-token"; then exit 1; fi
grep -q -- 'bot token is required' "$root/missing-token"

if FAKE_ACTOR_ID='' FAKE_STDIN="$root/missing-actor-stdin" \
  DOCKER_BIN="$fake_bin/docker" STAGING_ENV_FILE="$root/env" STAGING_COMPOSE_FILE="$root/compose.yaml" \
  FAKE_DOCKER_LOG="$root/missing-actor-docker.log" FAKE_SQL="$root/missing-actor-sql" \
  "$script_dir/admin-bootstrap.sh" --bot buyer --bot-username 'BuyerBot' --actor-username 'Missing Admin' \
  >/dev/null 2>"$root/missing-super" <<'EOF'
123456:token-for-missing-actor
EOF
then
  exit 1
fi
grep -q -- 'exactly one active super administrator was required' "$root/missing-super"
echo 'admin bootstrap script: PASS'
