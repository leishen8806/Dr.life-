# Public cache

Only `catalog:`, `config:`, and `i18n:` keys are accepted. The cache rejects `ledger:`, `wallet:`, `order:`, `stock:`, `withdraw:`, and every unregistered namespace for reads, writes, deletes, and loads, because public caching must never carry financial, inventory, personal data, sessions, tokens, secrets, or chat content.

Keys are stored as `drlife:<APP_ENV>:v1:<namespace>:<name>` and may contain only the registered namespace plus a lowercase ID without a colon. `CACHE_STALE_TTL_SECONDS` and `CACHE_NEGATIVE_TTL_SECONDS` both default to 30 seconds; `CACHE_MAX_ENTRIES` defaults to 10000; `CACHE_REFRESH_TIMEOUT` defaults to 2 seconds and is capped at 10 seconds. Values above 64KiB are skipped without error and counted in `drlife_cache_oversize_skips_total`.

`Remember` coalesces concurrent source loads, serves stale values while refreshing, and negative-caches `ErrNotFound`. Its shared source context is independent of callers and bounded by `CACHE_REFRESH_TIMEOUT`. The Redis backend is enabled only with `CACHE_REDIS_URL`; it must be a bounded, dedicated `allkeys-lru` instance. It never falls back to `REDIS_URL`, which remains for sessions, step-up, idempotency, and rate limiting under `noeviction`.
