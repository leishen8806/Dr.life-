# queuectl

Operational CLI for the Dr.life+ asynq queues. It talks directly to the queue
Redis instance and, for `outbox lag`, to PostgreSQL in read-only mode.

## Access boundary

- Run this tool only from the WireGuard operations network.
- Do **not** expose it through Cloudflare Tunnel and do not add a web UI.
- Output never includes task payloads, phone numbers, chat text, or last task
  errors. Only queue names, task IDs, registered task types, counts, and ages
  are printed.

## Commands

| Command | Flags | Result |
| --- | --- | --- |
| `queuectl stats` | - | JSON array with depth (`pending`, `active`, `scheduled`, `retry`), dead letters, processed/failed totals, paused state, and oldest wait seconds per queue |
| `queuectl pause` | `--queue <name>` | Pauses consumption of one queue |
| `queuectl resume` | `--queue <name>` | Resumes consumption of one queue |
| `queuectl dead list` | `--queue <name>` | JSON array of archived tasks: ID, type, queue, retries, last failure time |
| `queuectl dead replay` | `--queue <name> --yes` | Moves archived tasks back to pending; prints a warning before acting |
| `queuectl dead delete` | `--queue <name> --yes` | Permanently deletes archived tasks; prints a warning before acting |
| `queuectl outbox lag` | - | JSON with `pending` and `oldest_age_seconds` for unsent `ledger.outbox` rows |

Queue names are the fixed registry: `critical`, `default`, `notify`, `ai`,
`batch`, `scheduled`. An unknown queue is a usage error.

`dead replay` and `dead delete` refuse to run without `--yes` (exit code 2).
Replay only changes task state; it never edits a payload.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Runtime/configuration error (Redis or PostgreSQL unreachable, invalid URL, missing required environment value) |
| 2 | Usage error (`--queue` missing/unknown, bad flag, destructive command without `--yes`) |

## Environment

| Variable | Used by |
| --- | --- |
| `APP_ENV` | Required by the shared configuration loader |
| `QUEUE_REDIS_URL` | `stats`, `pause`, `resume`, `dead *` |
| `DATABASE_URL` | `outbox lag` |

## Metrics and alerts

The queue collector (`backend/internal/platform/queue/metrics.go`) exposes:

| Metric | Labels |
| --- | --- |
| `drlife_queue_depth` | `queue` |
| `drlife_queue_oldest_task_seconds` | `queue` |
| `drlife_queue_retry_tasks` | `queue` |
| `drlife_queue_dead_letters` | `queue` |
| `drlife_queue_paused` | `queue` |
| `drlife_queue_tasks_total` | `queue`, `result` |
| `drlife_queue_task_duration_seconds` | `queue`, `type` |
| `drlife_queue_collector_up` | `queue` |
| `drlife_queue_redis_memory_used_bytes` | - |
| `drlife_queue_redis_memory_ratio` | - |
| `drlife_outbox_pending` | - |
| `drlife_outbox_oldest_seconds` | - |
| `drlife_scheduler_last_run_timestamp_seconds` | - |

Task IDs, user IDs, and payload values are never used as labels. Task types are
the registered queue types only; unregistered types are grouped as
`unregistered`.

## Process wiring

- `cmd/server` registers the collector on the internal metrics listener and
  starts the asynq scheduler when `QUEUE_REDIS_URL` is set. Both application
  instances run the scheduler; fixed task IDs make duplicate enqueues harmless.
- `cmd/worker` passes the same collector to asynq as the task observer, so task
  durations are recorded by queue and registered type.
- `outbox lag` reads `ledger.outbox` directly in read-only mode. This is an
  accepted shortcut until the ledger module exposes a read API.

Alert rules live in `deploy/observability/prometheus/alerts.yml`: dead-letter
growth, per-queue wait SLA (`critical` 10s, `default` 60s, `notify` 120s;
`batch` never alerts), outbox lag above 60s, queue Redis memory above 70%,
collector down, and scheduler missed ticks.

## Verification

```sh
cd backend
go vet ./...
go test ./cmd/queuectl/... ./internal/platform/queue/... -count=1
go test ./... -count=1
```

`-race` needs a C compiler; when cgo is available add `-race` to the test
commands.

Validate the rules with the Prometheus container (no local promtool needed):

```sh
docker run --rm --entrypoint promtool \
  -v "$PWD/deploy/observability/prometheus:/w" \
  prom/prometheus check rules /w/alerts.yml
```
