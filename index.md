# PR #235 evidence

Evidence was captured from PR #235 head `f56e905` after the B6 rework.

## Matrix

The 12 screenshots cover 430px buyer home, merchant order list, and merchant order detail for light/dark themes and Chinese/Khmer locales:

- `screenshots/buyer-home-430-{light,dark}-{zh,km}.png`
- `screenshots/merchant-orders-430-{light,dark}-{zh,km}.png`
- `screenshots/merchant-order-detail-430-{light,dark}-{zh,km}.png`

The merchant detail fixture includes a money amount rendered through the shared e4 formatter and the masked phone `****1234`. The capture waits through the startup gate, asserts the light/dark root background, verifies selected-language highlighting, dark language/status contrast at least 4.5:1, light buyer action contrast at least 4.5:1, no horizontal overflow, no order badge/summary overlap, and no merchant header brand/action overlap before saving each screenshot. Dark/light pairs have distinct SHA-256 hashes.

## Verification

- PASS: all 12 captures completed from the PR #235 head.
- PASS: buyer home is captured in Chinese and Khmer for both themes.
- PASS: merchant order list and order detail are captured in Chinese and Khmer for both themes.
- PASS: PR #235 implementation commit `f56e905` was pushed once; this evidence is isolated on `evidence/pr-235`.
- PASS: language and refresh controls use the shared `Button`; dark neutral status and unselected language controls pass the scripted 4.5:1 contrast check.
- PASS: header action labels stay unbroken at 430px while the action row wraps below the merchant brand as needed.
- PASS: order detail line items use separated description/amount columns; order lists have no default bullets, place the status badge below the order summary without overlap, and ellipsize long IDs; 16 status filters stay in one horizontal scrolling row.
