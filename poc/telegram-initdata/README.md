# Telegram initData + Bot pool PoC

This module is isolated from production code. It validates Telegram Mini App `initData`, tries each token in the selected end's Bot pool, and exposes `POST /poc/auth/verify`.

## Run

Put test-only tokens in `.env.local` (ignored by the repository) using the variable names below. Never commit or paste the file.

```text
POC_TG_BUYER_PRIMARY_TOKEN=
POC_TG_BUYER_STANDBY_TOKEN=
POC_TG_MERCHANT_PRIMARY_TOKEN=
POC_TG_MERCHANT_STANDBY_TOKEN=
POC_TG_DRIVER_PRIMARY_TOKEN=
POC_TG_DRIVER_STANDBY_TOKEN=
```

Load the file in a POSIX shell, then start the PoC:

```sh
set -a
. ./.env.local
set +a
go run ./cmd/server
```

The PoC serves `web/index.html` and the API from the same process. Put an HTTPS reverse proxy or temporary Cloudflare Tunnel in front of it, then configure that HTTPS URL as a test Bot's Mini App URL. The page sends `initData` only to the same-origin verification endpoint.

## Verify on a real Telegram client

1. Open the configured Mini App on a physical device.
2. Confirm the page shows a nonzero length and a 12-character prefix.
3. Select the Bot's end and press **Verify**.
4. Record only the length, `hit_bot`, `tried_count`, Telegram user ID suffix, and result; do not record raw `initData`.
5. Repeat through the standby Bot. Remove the primary token from the environment and restart: standby data must still pass, while primary data must fail.

The two webhook commands are compileable library comparisons. They read `BOT_TOKEN`, `WEBHOOK_DOMAIN`, and `WEBHOOK_SECRET` only from the environment and reply `ready` to `/start`.
