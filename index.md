# PR #235 evidence

Evidence was captured from PR #235 head `3381d28a8e4b21416d76556b489a4e15a774ad3d` without changing that branch.

## Matrix

The 12 screenshots cover 430px buyer home, merchant order list, and merchant order detail for light/dark themes and Chinese/Khmer locales:

- `screenshots/buyer-home-430-{light,dark}-{zh,km}.png`
- `screenshots/merchant-orders-430-{light,dark}-{zh,km}.png`
- `screenshots/merchant-order-detail-430-{light,dark}-{zh,km}.png`

The merchant detail fixture includes a money amount rendered through the shared e4 formatter and the masked phone `****1234`. The capture waits through the startup gate and asserts the business-page controls before saving each screenshot.

## Verification

- PASS: all 12 captures completed from the PR #235 head.
- PASS: buyer home is captured in Chinese and Khmer for both themes.
- PASS: merchant order list and order detail are captured in Chinese and Khmer for both themes.
- PASS: no PR #235 branch commit was pushed; this evidence is isolated on `evidence/pr-235`.
