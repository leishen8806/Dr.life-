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

Unknown `/v1` audiences and cross-origin requests outside the configured allowlist fail closed. `healthz` and `readyz` remain public and bypass rate limits. Until A-03 supplies a production `Authenticator`, protected paths return 401. The IP resolver trusts a configured forwarding header only when the direct peer belongs to a configured trusted proxy CIDR.

The rate limiter uses fixed windows because they need one atomic increment and expiry in Redis. This is simpler to operate than a sliding log and may permit a short burst at a window boundary. If Redis returns an error, the server logs a warning and falls back to a process-local memory limiter so a cache outage does not take down the API; limits are then per instance until Redis recovers.
