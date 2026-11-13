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
   by `db-bootstrap`.

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
3. Start the `edge` service in `compose.yaml`. It is the only service exposing
   host ports: 80 redirects to 443, and 443 routes by hostname to `api:8080`,
   `buyer:80`, `merchant:80`, and `admin:80`. All other services stay internal.
4. Restrict the server firewall to TCP 80/443 from the published Cloudflare IP
   ranges. `test-admin` is protected by its own administrator account plus
   TOTP (E-04); do not issue an admin account before E-04 is complete. The API
   and Mini App hosts must remain reachable by Telegram.
5. The edge adds HSTS, `X-Content-Type-Options`, `Referrer-Policy`, and
   `frame-ancestors 'none'` to all four hosts. The API admin-auth path has a
   configurable nginx limit of 10 requests/minute per IP with a burst of 5 and
   returns 429 when exceeded. Harden SSH with key-only authentication, no root
   login, no password login, and fail2ban. If a fixed source IP is available,
   restrict SSH to it. The workflow uses `STAGING_SSH_HOST`, `STAGING_SSH_USER`,
   `STAGING_SSH_PRIVATE_KEY`, and `STAGING_SSH_KNOWN_HOSTS`; known_hosts is
   written and `StrictHostKeyChecking=yes` is required.
6. Run `deploy/staging/harden-server.sh --dry-run` first. Only the owner runs
   `--apply` on the server, after confirming a usable public-key SSH session;
   keep the current SSH connection open and verify a second login before
   closing the first. The script writes an isolated sshd drop-in, validates
   with `sshd -t` before restart, enables fail2ban, and checks the owner-managed
   Origin certificate and key are existing mode-600 files. To roll back, remove
   the drop-in after validating the restored config, reload sshd, and remove the
   matching ufw rules (or restore the owner-recorded ufw status). Never commit
   the generated rules or paths.
7. Verify each host without recording real output or addresses:
   `curl -I https://test-api.<placeholder>` must return the API health status;
   `curl -I https://test-miniapp.<placeholder>` and
   `curl -I https://test-shop.<placeholder>` must return 200/3xx; the admin
   host must pass its own admin login/TOTP; SSH must connect only with the
   owner-managed key and known_hosts entry.

## Backup, restore, and operations

The owner schedules a daily full PostgreSQL backup and a restore drill at least
once per week in the approved environment. Check `docker compose ps`, API
health, worker logs, Redis disk/AOF health, and PostgreSQL backup freshness.
Do not paste credentials, tunnel tokens, customer data, or server addresses in
logs or issues. Escalate incidents to `leishen8806`.

## Workflow

`.github/workflows/staging.yml` is manual, uses the `staging` protected
environment, and references only Environment Secrets. It is a template: the
owner supplies the direct SSH host policy after review.
