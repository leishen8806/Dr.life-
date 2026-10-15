# Gateway middleware

The gateway is the common trust boundary for every API client. The server installs middleware in this order:

1. request ID and security response headers;
2. metrics, access logging, and panic recovery;
3. path-grouped CORS, client IP rate limit, and admin/WMS network allowlist;
4. webhook verification;
5. authentication and path-to-role authorization;
6. request-body policy and account rate limit;
7. idempotency-key validation and request-bound idempotency;
8. router and any route-level step-up verifier.

Unknown `/v1` audiences and all non-`/v1` paths fail closed. Only `/healthz`, `/readyz`, and an explicitly configured `PUBLIC_WEBHOOK_PREFIX` are public. A request under the webhook prefix is still rejected unless its exact route is bound to a `WebhookVerifier`.

## Browser and transport boundary

CORS origins are configured independently for buyer, merchant, driver, WMS, admin, and public paths. Production defaults match the domains in `Dr.life+API安全协议与容量及队列设计.md`; `APP_ENV=dev` additionally allows `http://localhost:5173`. Cross-client origins are rejected, wildcard origins are never returned, and credentials are enabled only for exact paths in `CORS_CREDENTIAL_PATHS` (currently empty).

Every response receives `nosniff`, `no-referrer`, API-only CSP, same-site CORP, and `Cache-Control: no-store`. HSTS is enabled outside development. A public GET handler may explicitly replace `no-store` when its response is approved for caching. The HTTP server caps headers at 16 KiB. JSON writes default to 64 KiB, require `application/json`, and can use `httpx.DecodeJSON` to reject unknown fields and trailing JSON. Explicit upload/webhook exceptions and per-route byte limits are configured through `CONTENT_TYPE_EXEMPT_PATHS` and `REQUEST_BODY_OVERRIDES`.

## Authentication, network, and rate limits

Authentication failures share the same external error and `WWW-Authenticate: Bearer`; authenticators may keep internal failure reasons in redacted logs. `/v1/admin/*` and `/v1/wms/*` require a source CIDR in `ADMIN_ALLOWED_CIDRS`. An empty list allows development traffic but denies all non-development traffic. Forwarded client IP is used only when the direct peer is in `TRUSTED_PROXY_CIDRS`.

The unauthenticated IP default is 600 requests per minute because Cambodian mobile carrier NAT can place many users behind one public IPv4 address. IPv6 clients are grouped by `/64`; IPv4 addresses remain distinct. Login, withdrawal, AI, and admin/WMS path segments use stricter limits. The limiter uses fixed windows, so a client can send up to about twice the limit across a window boundary. General rate limiting falls back to process-local counters during Redis failure; financial idempotency does not fall back and returns 503.

## Idempotency and step-up

Every non-exempt write requires an `Idempotency-Key`. The store scope is account + method + route pattern + key, and the stored SHA-256 binds that key to the request body for 24 hours. Completed small responses are replayed with `Idempotent-Replayed: true`; a changed body returns 422, an in-progress request returns 409 with `Retry-After`, and 5xx responses release the key for retry. Responses larger than `IDEMPOTENCY_MAX_RESPONSE_BYTES` are recorded without their body and later return the uniform already-processed error. The current financial prefixes are configuration placeholders and must be updated when final business routes are added.

High-risk routes wrap their handler with `RequireStepUp(scope, verifier, jtiStore)`. A future A-series verifier validates signature, scope, account, age, and returns the JTI; the gateway consumes that JTI once in Redis and sets a private context marker. Request headers cannot set the marker. Completed idempotent replays happen before a route handler and therefore do not consume another step-up token.

## Webhooks

`VerifyWebhooks` supports exact-route bindings for Telegram-style fixed header secrets, HMAC-SHA256 over body + timestamp with nonce replay protection, and source CIDRs. HMAC timestamps default to a five-minute tolerance and nonces are held for ten minutes. Verification errors are uniform and must never log secrets or full request bodies.

## Configuration

See `backend/.env.example` for the complete list. Security-sensitive defaults are:

- request body and webhook body: 65,536 bytes;
- idempotency TTL: 24 hours; cached response: 65,536 bytes;
- CORS preflight cache: 600 seconds;
- admin/WMS CIDRs: empty, fail closed outside development;
- credential-bearing CORS paths: empty.

HSTS `preload`, the final financial route prefixes, and the response cache size remain product and operations confirmations.
