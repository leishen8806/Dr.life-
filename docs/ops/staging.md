# Staging deployment template

This directory contains placeholders only. It does not connect to a server and
must not be run with production values. The owner creates `deploy/staging/.env`
from `env.example` in the approved staging environment; `.env` is ignored.

## First deployment

1. Copy `env.example` to `.env`, change `STAGING_ENV_FILE` to `.env`, and
   replace every `CHANGE_ME` value in the owner-managed secret store. Keep the
   three Redis URLs on separate services:
   state and queue use AOF/noeviction; cache uses allkeys-lru.
2. Run `backend/scripts/db-bootstrap` once with the owner-managed role
   passwords. It is the single source of truth for login roles and least
   privilege grants; `init.sql` intentionally does not create duplicate roles.
3. Review the generated compose configuration:
   `docker compose --env-file .env -f compose.yaml config --quiet`.
4. Run the migration as an explicit step. Compose maps the owner-managed
   `MIGRATION_DATABASE_URL` to the migration binary's required `DATABASE_URL`
   and does not pass the application `.env` wholesale:
   `docker compose --profile ops run --rm migrate`.
5. Start the stack. The API health check is `/healthz`; worker and routefitmock
   are separate processes. The migration connection uses the owner-managed
   PostgreSQL migration credential in `.env`; application URLs use roles created
   by `db-bootstrap`. In the test environment, the worker uses the dedicated
   `app_ledger` database role via `WORKER_DATABASE_URL`; the production design
   remains pending owner approval.

### Host nginx change procedure (方案 b)

Keep the existing official-site server blocks unchanged. Before installing the
four-name staging proxy, record a backup and the official-site baseline:

```bash
backup_dir="/var/backups/drlifeplus-nginx-$(date -u +%Y%m%dT%H%M%SZ)"
sudo install -d -m 700 "$backup_dir"
sudo cp -a /etc/nginx "$backup_dir/"
curl --silent --show-error --location --max-time 10 -o /tmp/drlifeplus-baseline.html -w '%{http_code}\n' https://drlifeplus.com/
curl --silent --show-error --location --max-time 10 -o /tmp/drlifeplus-www-baseline.html -w '%{http_code}\n' https://www.drlifeplus.com/
```

After reviewing the owner-managed certificate paths and generated
`cloudflare-real-ip.conf`, install the separate host-proxy entry, validate, and
reload (do not restart):

```bash
sudo install -m 644 deploy/staging/nginx/host-proxy.conf.example /etc/nginx/sites-enabled/drlifeplus-staging.conf
sudo nginx -t
sudo systemctl reload nginx
curl --silent --show-error --location --max-time 10 -o /tmp/drlifeplus-after.html -w '%{http_code}\n' https://drlifeplus.com/
curl --silent --show-error --location --max-time 10 -o /tmp/drlifeplus-www-after.html -w '%{http_code}\n' https://www.drlifeplus.com/
```

Compare the two official pages' status and `<title>` with the baseline before
continuing. If validation, reload, or the baseline comparison fails, remove
the new entry and restore the backup, then validate and reload again:

```bash
sudo rm -f /etc/nginx/sites-enabled/drlifeplus-staging.conf
sudo cp -a "$backup_dir/nginx/." /etc/nginx/
sudo nginx -t
sudo systemctl reload nginx
```

The template uses `proxy_ssl_verify off` only for the loopback hop from the
host nginx to the staging edge. It is bounded to `127.0.0.1`, while the
public-facing TLS certificate and Cloudflare Full (strict) validation remain
on the host proxy; do not reuse this setting for an external upstream.

## Release and rollback

Upload a built static release to `incoming/<release-id>` below the selected
application directory (`buyer`, `merchant`, or `admin`), then run
`publish-release.sh <app> <release-id>`. It atomically switches that app's
`current` symlink and keeps the five newest releases for that app. Run
`rollback-release.sh <app>` to switch that app to its previous release.
`deploy/staging/test-release-scripts.sh` exercises publish and rollback for all
three applications with a temporary root. Database migration is deliberately
separate and must succeed before the application release is switched.

## E-01 domain direct-connection configuration

This is a template only. Do not put real domains, certificate material,
credentials, or firewall data in the repository.

1. Create four Cloudflare DNS A records for placeholder hosts `test-api`,
   `test-miniapp`, `test-shop`, and `test-admin`; enable the orange-cloud proxy.
2. Set Cloudflare SSL/TLS to **Full (strict)**. Owner-managed Cloudflare
   Origin certificate and key files are mounted through
   `STAGING_ORIGIN_CERT_PATH` and `STAGING_ORIGIN_KEY_PATH`; they never enter
   Git or the image.
3. Keep the existing host nginx on 80/443 for the official website. Install the
   reviewed `nginx/host-proxy.conf.example` as a separate host configuration
   for the four test names, and start the `edge` service in `compose.yaml` with
   loopback-only ports (`127.0.0.1:18080:80` and `127.0.0.1:18443:443`). The
   host nginx terminates the Origin certificate and proxies the test names to
   the edge HTTPS listener; edge still routes by hostname to `api:8080`,
   `buyer:80`, `merchant:80`, and `admin:80`. All other services stay internal.
4. Restrict the server firewall to TCP 80/443 from the published Cloudflare IP
   ranges. `test-admin` is protected by its own administrator account plus
   TOTP (E-04); do not issue an admin account before E-04 is complete. The API
   and Mini App hosts must remain reachable by Telegram.
5. The edge adds HSTS, `X-Content-Type-Options`, `Referrer-Policy`, and
   `frame-ancestors 'none'` to all four hosts. Before starting the edge,
   `harden-server.sh --apply --ssh-user=<owner-login-user>` generates
   `STAGING_CLOUDFLARE_REAL_IP_CONFIG` from the current Cloudflare ranges and
   exactly one staging edge Docker gateway address; both the host proxy and
   edge include it. Nginx then uses `CF-Connecting-IP` and
   `$binary_remote_addr` is the real client address. The API admin-auth path's explicit limit is in
   `nginx/staging.conf` (`10r/m`, burst `5`); change both values there as one
   reviewed configuration change. It returns 429 when exceeded. Harden SSH
   with key-only authentication, no root login, no password login, and
   fail2ban. If a fixed source IP is available,
   restrict SSH to it. The workflow uses `STAGING_SSH_HOST`, `STAGING_SSH_USER`,
   `STAGING_SSH_PRIVATE_KEY`, and `STAGING_SSH_KNOWN_HOSTS`; known_hosts is
   written and `StrictHostKeyChecking=yes` is required.
6. Run `deploy/staging/harden-server.sh --dry-run` first. Only the owner runs
   `--apply --ssh-user=<owner-login-user>` on the server, after confirming a usable public-key SSH session;
   keep the current SSH connection open and verify a second login before
   closing the first. The script writes an isolated sshd drop-in, validates
   with `sshd -t` before restart, enables fail2ban, and checks the owner-managed
   Origin certificate and key are existing mode-600 files. To roll back, remove
   the drop-in after validating the restored config, reload sshd, and remove the
   matching ufw rules (or restore the owner-recorded ufw status). The script
   backs up `/etc/ufw/after.rules` and `after6.rules` under
   `/var/backups/drlifeplus-staging-<timestamp>/` before reset. Never commit
   the generated rules or paths.
7. Verify each host without recording real output or addresses:
   `curl -I https://test-api.<placeholder>` must return the API health status;
   `curl -I https://test-miniapp.<placeholder>` and
   `curl -I https://test-shop.<placeholder>` must return 200/3xx; the admin
   host must pass its own admin login/TOTP; SSH must connect only with the
   owner-managed key and known_hosts entry. Confirm a request from a
   non-Cloudflare source to the server IP on TCP 80/443 is refused (Docker
   published ports are filtered by the generated `DOCKER-USER` rules, not
   only UFW INPUT rules).

## Backup, restore, and operations

### 首次初始化后台超管

部署包含 `admininit` 的镜像后，由 owner 使用 deploy 用户在测试服务器执行：

```sh
STAGING_COMPOSE_FILE=compose.yaml DOCKER_USE_SUDO=1 \
  deploy/staging/admin-bootstrap.sh --username '<owner-chosen-name>'
```

Run this from the staging project directory containing `compose.yaml`. The
`STAGING_COMPOSE_FILE` override is explicit so the same command also works
when the script is invoked from a checkout. If the `deploy` user is not in the
Docker group, set `DOCKER_USE_SUDO=1`; the script then invokes `sudo docker`
without printing the temporary bootstrap credentials.

脚本创建只继承 `iam_rw` 与 `audit_writer` 的一次性数据库登录，密码只通过进程内存传给容器；无论成功或失败都会在 `trap` 中删除该登录。`admininit` 的初始密码和 `otpauth://` 只显示在执行人的终端，不写日志、不写文件、不接受密码参数。执行完成后只在 #276 评论“第 0 步已完成”，不要贴密码、链接或截图。

The **owner** executes step 0 after the PR is merged and the test deployment is ready. The owner schedules a daily full PostgreSQL backup and a restore drill at least
once per week in the approved environment. Check `docker compose ps`, API
health, worker logs, Redis disk/AOF health, and PostgreSQL backup freshness.
Do not paste credentials, tunnel tokens, customer data, or server addresses in
logs or issues. Escalate incidents to `leishen8806`.

For E-09.4, first run `deploy/staging/worker-healthcheck.sh` to confirm that
the asynq v0.26 worker heartbeat exists in the `asynq:servers` ZSET and that
its expiry score advances during the observation window. Then run
`deploy/staging/restore-drill.sh --dry-run` with
`SOURCE_DATABASE_URL` and `RESTORE_DATABASE_URL` set to an approved test
environment. Then rerun without `--dry-run`: the script takes a custom-format
`pg_dump`, recreates only the `drlife_restore_drill_*` target, restores it,
compares every non-system schema/table row count, and drops the temporary
database from an exit trap. A mismatch or cleanup failure is FAIL; credentials
remain in environment variables and are never printed.

For the E-09 test-environment acceptance record, use
[`test-env-acceptance.md`](./test-env-acceptance.md). The repository template
provides `smoke.sh`, `worker-healthcheck.sh`, and `restore-drill.sh`; all
require explicit environment
variables and must be run only with approved test-environment values.

## Workflow

`.github/workflows/staging.yml` is manual, uses the `staging` protected
environment, and references only Environment Secrets. It is a template: the
owner supplies the direct SSH host policy after review.

The production website verification step in `.github/workflows/website.yml` depends on the deploy job checking out `.github/scripts` with `persist-credentials: false`; this checkout must remain the first step of `deploy-production`, before any files are downloaded or generated.
