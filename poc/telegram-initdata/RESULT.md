# P0-1 Telegram initData + Bot pool result

Checked on **2026-10-12**. This is a technical PoC; nothing under `backend/`, OpenAPI, or migrations is changed.

## Result summary

| Item | Result |
|---|---|
| Token HMAC algorithm | Implemented and covered by synthetic tests |
| `signature` handling | Included in the Bot-token HMAC data-check-string; only `hash` is excluded |
| Expiry / future timestamp | Default 1 hour / 60 seconds, tested |
| Pool fallback and cross-end isolation | Tested with synthetic tokens |
| Constant-time comparison / 4 KiB limit / malformed input | Implemented; unit, boundary, and fuzz coverage |
| Real Telegram client `initData` | **Pending user verification**; no Bot token or physical-device session was available |
| Revoked real primary Bot and real standby Bot | **Pending user verification** |

Local verification used `golang:1.27.2` in Docker Desktop 4.93.0 / Engine 29.8.1 on Linux with 8,322,220,032 bytes assigned memory:

```text
go vet ./...                         passed
go test -race -count=1 ./...         passed
fuzz FuzzValidateDoesNotPanic, 5 s   passed, 201667 executions
go list -m all                       module plus the two compared Bot libraries only
```

The current Telegram documentation defines two different checks:

- For a Bot backend with the Bot token, the sorted HMAC data-check-string excludes `hash` only. Therefore the newer `signature` field, when present, remains in this HMAC input.
- Third-party Ed25519 validation excludes both `hash` and `signature` and prepends `<bot_id>:WebAppData`. This PoC is the Bot backend case and does not implement the third-party path.

Sources checked on 2026-10-12:

- [Telegram Mini Apps: Validating data received via the Mini App](https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app)
- [Telegram Mini Apps: Validating data for Third-Party Use](https://core.telegram.org/bots/webapps#validating-data-for-third-party-use)

## Library comparison

Both examples use webhook secret-token validation and reply only to `/start`. Neither library supplies the Bot-token Mini App `initData` validation used here, so the PoC keeps the short, auditable standard-library implementation.

| Area | `gotgbot` | `go-telegram/bot` |
|---|---|---|
| Version checked | `v2.0.0-rc.36`, tagged 2026-08-02; `v2` branch last commit 2026-09-19 | stable `v1.27.0`, released 2026-09-11; main last commit 2026-10-08 |
| Webhook and `secret_token` | `ext.WebhookOpts.SecretToken`; library server verifies the Telegram header | `WithWebhookSecretToken`; `WebhookHandler` verifies the Telegram header |
| Mini App menu/button | Generated Bot API types and `SetChatMenuButton`; sample Web App exists | Generated `MenuButtonWebApp`, `WebAppInfo`, and `SetChatMenuButton` types/method |
| Dependencies | No runtime third-party transitive dependency at the pinned version | No runtime third-party transitive dependency at the pinned version |
| Testability | Bot options accept a custom HTTP client; dispatcher is feature-rich but broader | `WithHTTPClient`, `ProcessUpdate`, and small `net/http` handler make focused tests direct |
| Fanout / limits | Exposes Bot API errors; application must queue, observe `retry_after`, and control per-chat/global pacing | Same: worker count controls local update handling, but outbound fanout and 429 policy remain application work |
| Maintenance signal | Active commits, but v2 remains a release candidate | Active commits and a stable release line |

**Recommendation: `go-telegram/bot`.** It has a stable current release, a small standard-library-shaped webhook surface, built-in webhook secret verification, injectable HTTP transport, worker controls, and no runtime transitive dependencies. `gotgbot` is capable and has a richer dispatcher, but adopting an RC for the first production Bot layer adds avoidable upgrade risk.

Official repository evidence:

- [`gotgbot` repository](https://github.com/PaulSonOfLars/gotgbot), [`v2.0.0-rc.36` tag](https://github.com/PaulSonOfLars/gotgbot/releases/tag/v2.0.0-rc.36), [official webhook sample](https://github.com/PaulSonOfLars/gotgbot/tree/v2/samples/echoWebhookBot), [official Web App sample](https://github.com/PaulSonOfLars/gotgbot/tree/v2/samples/webappBot)
- [`go-telegram/bot` repository](https://github.com/go-telegram/bot), [`v1.27.0` release](https://github.com/go-telegram/bot/releases/tag/v1.27.0), [official webhook sample](https://github.com/go-telegram/bot/tree/main/examples/echo_with_webhook), [`WebhookHandler` source](https://github.com/go-telegram/bot/blob/main/webhook_handler.go)

## Notification reachability experiment

Hypothesis: a standby Bot improves reach only for users who already pressed `/start` for that Bot.

1. Recruit 20 test accounts and record whether each has pressed `/start` on primary and standby Bots.
2. Send one non-sensitive test notification through the primary Bot at a controlled rate.
3. Record delivered, Telegram 403 (blocked or chat unavailable), accounts that never started the Bot, 429 and `retry_after`, and other failures. Do not record message text or profile data.
4. Repeat with the standby Bot only for accounts bound to it; respect `retry_after` and do not bypass user blocks.
5. Report delivery rate by binding state and reason counts. Recommend standby routing only among Bots the account has started; otherwise use in-app notification and, for approved financial notices, the separately governed SMS fallback.

Suggested success signal: at least 95% delivery among accounts that started the selected Bot, zero attempted fallback through a Bot the account never started, and no repeated request before Telegram's `retry_after` expires. A 20-account sample is directional rather than a production SLA.

## Real-client evidence to add

After the user performs the README steps, record without raw initData or tokens:

```text
Status: pending user verification
Device/client: pending
initData length: pending
Matched Bot: pending (end + primary/standby only)
tried_count: pending
Telegram user ID: last four digits only, pending
Primary removal / standby result: pending
```

The production A-02 implementation should reuse the verified algorithm and pool isolation behavior after this real-client gate passes. It must obtain encrypted tokens through the production secret boundary rather than environment-backed PoC configuration.

Documentation follow-up: after the real-client and real token-revocation checks pass, update the PoC row in `Dr.life+技术方案与系统边界.md` with the measured evidence and this library recommendation. It remains unchanged in this PR because the external acceptance evidence is still pending.
