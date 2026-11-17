# Production worker database role design

This document defines the review boundary for a production worker login. It is
design and deployment guidance only: this PR does not create a production
role, change a migration, or contain a real database address or password.

## Scope

The worker login is intended for the background jobs that already have a
database-facing store. The current code separates these capabilities:

| worker capability | database surface | required access |
| --- | --- | --- |
| file verification | `ops.file` | `SELECT`; update only the state, object key, and timestamp columns used by `VerificationWorker` |
| file retention cleanup | `ops.file` and its retention/reference guards | the existing `ops_file_worker` grants from migrations `00031`, `00059`, and `00060`; do not widen them to the application role |
| ledger/outbox workers | `ledger.*` | use the existing `ledger_rw` role through a separately named login; do not grant the login direct ownership or superuser privileges |

The worker must not receive `iam_rw`, `catalog_rw`, `trade_rw`, or a broad
`ops_rw` grant merely because it runs in the same process. KESS and other
external-provider workers should use the narrow role required by their store
and be reviewed separately.

## Production execution checklist

1. Render the placeholders in `deploy/production/worker-grants.sql.example`
   from the secret manager during the owner-controlled deployment. Never put
   the rendered file in git or in CI logs.
2. Confirm the role is a `NOLOGIN` group role where possible, and grant the
   runtime login membership in that group. If a direct login is unavoidable,
   keep `INHERIT`, `NOSUPERUSER`, `NOCREATEDB`, `NOCREATEROLE`, and `NOREPLICATION`.
3. Verify `pg_has_role` and `information_schema.role_table_grants` using a
   disposable test database before rollout. In particular, assert that the
   worker cannot delete arbitrary `ops.file` rows and cannot write unrelated
   schemas.
4. Rotate the login credential through the secret manager, restart the worker,
   and run one non-destructive queue job. Record the migration/release ID and
   the observed database role in the deployment log.
5. Roll back by disabling the worker login or revoking its group membership;
   do not edit historical migrations in production.

## `admininit` database account

The owner-operated `admininit` command must use a disposable login with the
minimum existing role memberships required by the bootstrap path: `iam_rw` for
IAM reads/writes and `audit_writer` for audit inserts and the approved
`SECURITY DEFINER` chain-head functions. The login must inherit those group
roles and must not be the migration account, a superuser, or a database owner.
Using the migration account makes `current_user` equal to `session_user` and
causes the audit chain guard to reject the append. If a new login is needed,
create it through the owner-controlled secret/deployment process using the
placeholder pattern in `deploy/production/worker-grants.sql.example`; this PR
does not add production SQL or credentials.

## Evidence boundary

The integration tests intentionally create disposable logins such as
`ops_file_worker` and `app_ledger` with test-only credentials. They prove the
permission boundary in a temporary database, not production readiness. A
production rollout remains an owner-operated step after the grant set is
reviewed against the exact SQL used by the deployed commit.
