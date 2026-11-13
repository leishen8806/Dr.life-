# PR #235 evidence

Evidence was captured from PR #235 head `532a83d4960a49e2629064356fc093dcaebb3080` without changing that branch.

## Matrix

The 12 screenshots cover 430px buyer home, merchant order list, and merchant order detail for light/dark themes and Chinese/Khmer locales:

- `screenshots/buyer-home-430-{light,dark}-{zh,km}.png`
- `screenshots/merchant-orders-430-{light,dark}-{zh,km}.png`
- `screenshots/merchant-order-detail-430-{light,dark}-{zh,km}.png`

The merchant detail fixture includes a money amount rendered through the shared e4 formatter and the masked phone `****1234`. The capture waits through the startup gate, asserts the light/dark root background, and verifies the business-page controls before saving each screenshot. Dark/light pairs have distinct SHA-256 hashes.

## Verification

- PASS: all 12 captures completed from the PR #235 head.
- PASS: buyer home is captured in Chinese and Khmer for both themes.
- PASS: merchant order list and order detail are captured in Chinese and Khmer for both themes.
- PASS: PR #235 implementation commit `532a83d` was pushed once; this evidence is isolated on `evidence/pr-235`.
- PASS: order detail line items use separated description/amount columns; order lists have no default bullets and ellipsize long IDs; 16 status filters stay in one horizontal scrolling row.
