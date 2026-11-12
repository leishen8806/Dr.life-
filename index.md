# PR #233 screenshot evidence

Generated from Website workflow run `43228490592` for head `a4eb54a421faeca4c5671d02e1415e088b0da47b`.

All files in this branch are WebP and are below 300 KB each.

## Homepage (D/F)

The homepage captures cover the requested 360, 390, 768, 1280, 1440 and 1920 pixel widths in Chinese, English and Khmer:

- `site-home-{360,390,768,1280,1440,1920}-{zh,en,km}.webp`

The CI capture job also exercised the full homepage matrix at 360, 390, 768, 1280, 1440 and 1920 pixels for zh/en/km, with h1 visibility and horizontal-overflow assertions.

## Mini App matrix

The remaining 24 files are the CI-produced buyer/merchant matrix at 360/430 pixels, light/dark themes and zh/en/km locales. The CI job completed successfully in the same run and wrote `manifest.txt` before these files were converted to WebP. The largest evidence file is 211,352 bytes, below the 300 KB limit.

## Acceptance evidence

- Website screenshot evidence: PASS — run `43228490592`, current head `a4eb54a`.
- CI gate: IN_PROGRESS — run `43228490321`, current head `a4eb54a`.
- The screenshot job also asserted visible h1 content and no horizontal overflow at each viewport.
