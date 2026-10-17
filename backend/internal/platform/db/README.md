# Database pools

Each application server owns three independent PostgreSQL pools:

| Pool | Purpose | Default max | Statement timeout |
| --- | --- | ---: | --- |
| `oltp` | HTTP requests and ledger | 10 | 5 seconds |
| `report` | read-only reports | 4 | 30 seconds |
| `worker` | queue workers | 4 | 60 seconds |

All pools set `lock_timeout=3s` and `idle_in_transaction_session_timeout=10s` on every connection. Ledger posts use `SET LOCAL statement_timeout='15s'` inside their transaction only, so the connection returns to the `oltp` 5-second default after commit or rollback.

`DB_ACQUIRE_TIMEOUT_MILLISECONDS` defaults to 100 ms: callers using `db.Acquire` fail with a retryable error instead of waiting indefinitely.

`REPORT_DATABASE_URL` should point to a read replica. Until deployment provides one, it may point to the primary only with the existing read-only database role; that deployment decision remains open.

## Connection budget

For each application server:

`oltp_max + report_max + worker_max <= PostgreSQL max_connections * 0.40`

For the defaults, `10 + 4 + 4 = 18`. With PostgreSQL `max_connections=100`, the per-server budget is `100 * 0.40 = 40`, so 18 is within budget. Two application servers consume at most 36 connections, also below the 80-connection two-server allowance.

The defaults require S-05 load-test calibration before production rollout.
