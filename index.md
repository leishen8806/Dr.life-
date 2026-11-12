# PR #233 screenshot evidence

Generated from Website workflow run `43238382805` for head `d4a3ba2fee53042706799c629a799618166f742e`.

All 60 evidence files in this branch are WebP and are below 300 KB each.

## Website matrix (D/F)

The website captures cover the requested 360, 390, 768, 1280, 1440 and 1920 pixel widths in Chinese, English and Khmer for both the homepage and merchant page:

- `site-{home,merchant}-{360,390,768,1280,1440,1920}-{zh,en,km}.webp`

The CI capture job exercised the full website matrix with h1 visibility, failed-resource, and horizontal-overflow assertions.

## Mini App matrix

The remaining 24 files are the CI-produced buyer/merchant matrix at 360/430 pixels, light/dark themes and zh/en/km locales. The job completed successfully and wrote `manifest.txt` before these files were converted to WebP. The largest evidence file is 296,860 bytes, below the 300 KB limit.

## Acceptance evidence

- Website screenshot evidence: PASS — run `43238382805`, current head `d4a3ba2`.
- CI gate: PASS — run `43238382755`, current head `d4a3ba2`.
- The screenshot job also asserted visible h1 content and no horizontal overflow at each viewport.
