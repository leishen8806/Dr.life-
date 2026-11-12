# PR #233 screenshot evidence

Generated from Website workflow run `43207560995` for head `45d3b0591c4dfdf5217fc5d3f7a5a8c1b995996c`.

All files in this branch are WebP and are below 300 KB each.

## Homepage (D/F)

The homepage captures cover the requested 1920, 1440 and 390 pixel widths in Chinese and Khmer:

- `site-home-1920-zh.webp`, `site-home-1920-km.webp`
- `site-home-1440-zh.webp`, `site-home-1440-km.webp`
- `site-home-390-zh.webp`, `site-home-390-km.webp`

The CI capture job also exercised the full homepage matrix at 360, 390, 768, 1280, 1440 and 1920 pixels for zh/en/km, with h1 visibility and horizontal-overflow assertions.

## Mini App matrix

The remaining 24 files are the CI-produced buyer/merchant matrix at 360/430 pixels, light/dark themes and zh/en/km locales. The CI job completed successfully and wrote `manifest.txt` before these files were converted to WebP.
