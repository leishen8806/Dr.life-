# PR #233 screenshot evidence

Generated from the local capture scripts for PR head `f03e92a` (`[返工] #230 对齐官网合作与应用版块布局`).

All 72 evidence files in this branch are WebP and are below 300 KB each.

## Website matrix (D/F)

The website captures cover the requested 360, 390, 768, 1280, 1440 and 1920 pixel widths in Chinese, English and Khmer for both the homepage and merchant page:

- `site-{home,merchant}-{360,390,768,1280,1440,1920}-{zh,en,km}.webp`

The capture job exercised the full website matrix with h1 visibility, failed-resource, horizontal-overflow, hidden-until-focus skip-link, single-line 1920px navigation, aligned partner/access containers, packed partner/access grids, full-width four-column route-step geometry, and the 1920px Chinese route-title assertions.

## Mini App matrix

The remaining 36 files are the buyer home and order-list plus merchant home matrix at 360/430 pixels, light/dark themes and zh/en/km locales. The capture uses deterministic local API responses with one category and two products, waits through the splash gate, asserts business-page headings, verifies different light/dark root backgrounds, and writes the requested locale into the page before capture.

## Acceptance evidence

- Website screenshot evidence: PASS — 36 current-head captures for `f03e92a` at 360/390/768/1280/1440/1920 × zh/en/km × home/merchant.
- Mini App screenshot evidence: PASS — 36 captures retained from the immediately preceding `ed2f87d` head; this rework changes only website CSS and website screenshot assertions, not Mini App code.
- Local verification: PASS — website tests 50/50, UI tests 18/18, UI/buyer/website typechecks, UI/website lint, Astro build, website and Mini App screenshot captures, and `git diff --check`.
