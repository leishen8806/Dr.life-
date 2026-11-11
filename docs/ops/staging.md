# Staging deployment template

This directory contains placeholders only. It does not connect to a server and
must not be run with production values. The owner creates `deploy/staging/.env`
from `env.example` in the approved staging environment; `.env` is ignored.

## First deployment

1. Copy `env.example` to `.env` and replace every `CHANGE_ME` value in the
   owner-managed secret store. Keep the three Redis URLs on separate services:
   state and queue use AOF/noeviction; cache uses allkeys-lru.
2. Run `backend/scripts/db-bootstrap` once with the owner-managed role
   passwords. It is the single source of truth for login roles and least
   privilege grants; `init.sql` intentionally does not create duplicate roles.
3. Review the generated compose configuration:
   `docker compose --env-file .env -f compose.yaml config --quiet`.
4. Run the migration as an explicit step:
   `docker compose --profile ops run --rm migrate`.
5. Start the stack. The API health check is `/healthz`; worker and routefitmock
   are separate processes. The migration connection uses the owner-managed
   PostgreSQL migration credential in `.env`; application URLs use roles created
   by `db-bootstrap`.

## Release and rollback

Upload a built static release to `incoming/<release-id>`, then run
`publish-release.sh <release-id>`. It atomically switches `current` and keeps
the five newest releases. Run `rollback-release.sh` to switch to the previous
release. Database migration is deliberately separate and must succeed before
the application release is switched.

## Backup, restore, and operations

The owner schedules a daily full PostgreSQL backup and a restore drill at least
once per week in the approved environment. Check `docker compose ps`, API
health, worker logs, Redis disk/AOF health, and PostgreSQL backup freshness.
Do not paste credentials, tunnel tokens, customer data, or server addresses in
logs or issues. Escalate incidents to `leishen8806`.

## Workflow

`.github/workflows/staging.yml` is manual, uses the `staging` protected
environment, and references only Environment Secrets. It is a template: the
owner supplies the actual Tunnel SSH action and policy after review.
