# Gateway middleware

The server installs middleware in this order:

1. Request ID
2. Access log
3. Panic recovery
4. CORS allowlist
5. Client IP rate limit
6. Authentication
7. Path-to-role authorization
8. Account rate limit
9. Idempotency key validation
10. Router

Unknown `/v1` audiences and all non-`/v1` paths fail closed. Only `/healthz`, `/readyz`, and an explicitly configured `PUBLIC_WEBHOOK_PREFIX` are public; the prefix is empty by default, and a future Telegram webhook must apply its own authentication. Health checks bypass rate limits. Until A-03 supplies a production `Authenticator`, protected paths return 401. The IP resolver trusts a configured forwarding header only when the direct peer belongs to a configured trusted proxy CIDR.

The unauthenticated IP default is 600 requests per minute because Cambodian mobile carrier NAT can place many users behind one public IPv4 address. IPv6 clients are grouped by `/64`; IPv4 addresses remain distinct. Login, withdrawal, and AI path segments keep their stricter limits.

The rate limiter uses fixed windows because they need one atomic increment and expiry in Redis. This is simpler to operate than a sliding log, but a client can send up to about twice the limit across a window boundary. If Redis returns an error, the server logs one warning and falls back to process-local counters so a cache outage does not take down the API. During that fallback, each instance counts independently, so the effective fleet-wide limit is temporarily higher until Redis recovers.
