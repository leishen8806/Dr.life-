# httpx

Shared HTTP helpers.

## Public GET caching (S-02d)

- The gateway keeps `Cache-Control: no-store` as the default for every response.
- `CacheControl(true, maxAge, swr)` is opt-in and emits
  `public, max-age=<seconds>, stale-while-revalidate=<seconds>`; `CacheControl(false, ...)` keeps
  `no-store`. Install it inside `gateway.SecurityHeaders` so the explicit value wins.
- `ETag` only runs for `GET /v1/public/*`. It hashes the buffered body, emits a quoted SHA-256
  validator, and returns `304 Not Modified` with no body when `If-None-Match` matches. All other
  routes pass through untouched and keep `no-store`. `cmd/server/main.go` wraps the router with it,
  so new public GET routes get validators without extra wiring; `CacheControl` stays opt-in per route.

Do not declare a public route cacheable unless its response is safe to share between users.
