# PR #233 screenshot evidence

Generated from the local capture scripts for PR head `deb020a` (`fix(website): address B5 review findings`).

All 72 evidence files in this branch are WebP and are below 300 KB each.

## Website matrix (D/F)

The website captures cover the requested 360, 390, 768, 1280, 1440 and 1920 pixel widths in Chinese, English and Khmer for both the homepage and merchant page:

- `site-{home,merchant}-{360,390,768,1280,1440,1920}-{zh,en,km}.webp`

The capture job exercised the full website matrix with h1 visibility, failed-resource, horizontal-overflow, and the 1920px Chinese route-title assertions.

## Mini App matrix

The remaining 36 files are the buyer home and order-list plus merchant home matrix at 360/430 pixels, light/dark themes and zh/en/km locales. The capture uses deterministic local API responses, waits through the splash gate, asserts business-page headings, and writes the requested locale into the page before capture.

## Acceptance evidence

- Website screenshot evidence: PASS — 36 current-head captures for `deb020a`.
- Mini App screenshot evidence: PASS — 36 current-head captures for `deb020a`, including buyer home/order list and merchant home.
- Local verification: PASS — website tests 50/50, Astro build, screenshot captures, and `git diff --check`.
