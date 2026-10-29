# httpx

Shared HTTP helpers.

## Public GET caching (S-02d)

- The gateway keeps `Cache-Control: no-store` as the default for every response.
- `CacheControl(true, maxAge, swr, vary...)` is opt-in and emits
  `public, max-age=<seconds>, stale-while-revalidate=<seconds>`; `CacheControl(false, ...)` keeps
  `no-store`. Every request header that can change the response must be listed in `vary` (for
  example, `Accept-Language`); the helper adds each value without replacing existing `Vary`
  entries such as CORS's `Origin`. Install it inside `gateway.SecurityHeaders` so the explicit
  value wins.
- `ETag` only runs for `GET /v1/public/*`, and only when the route explicitly declares a public
  cache policy (normally by installing `CacheControl`). Public paths that never declare it keep
  `no-store` and receive no `ETag`. It hashes the buffered body with the per-request
  `request_id` removed, emits a quoted SHA-256 validator, and returns `304 Not Modified` with no
  body when `If-None-Match` matches. All other routes pass through untouched.
- Install order matters: `httpx.ETag(...)` wraps the router in `cmd/server/main.go`, while
  `CacheControl` stays inside `gateway.SecurityHeaders` on the route.
- Known limit: `304` is decided outside the metrics middleware, so
  `drlife_http_requests_total` still records the handler's `200` for those requests.

Do not declare a public route cacheable unless its response is safe to share between users and
every request-header variation is declared with `vary`.
