# PR #255 evidence

- Code head: `92e45b1` (latest fix commit on `feat/b14-seo-favicon`, based on merge `4c229ad`).
- Screenshot matrix: `screenshots/manifest.txt` and adjacent PNGs. The
  latest capture rerun completed with 78 screenshots, including 1440/390
  homepage and merchant routes for zh/en/km plus section captures.
- SEO metadata: [`curl-meta.txt`](curl-meta.txt), captured from the current
  preview build for `/`, `/en/`, `/km/`, `/merchant/`, `/en/merchant/`, and
  `/km/merchant/`, including raw title/meta/canonical/hreflang/OG/Twitter and
  homepage JSON-LD lines.
- Favicon assets: `assets/favicon.ico`, `assets/favicon.png`, `assets/apple-touch-icon.png`,
  `assets/icon-192.png`, `assets/icon-512.png`, and the OG JPG/WebP. Chrome tab
  and 16/32px comparison captures are in `screenshots/`.

## Acceptance evidence

- Build/static output: PASS (the website build produced the static routes).
- SEO route metadata: PASS for the four routes listed in `curl-meta.txt`.
- Screenshot generation: PASS (the repository capture script completed after
  the responsive header, eager image decode, and static-resource checks).
- Owner visual approval of favicon/logo and responsive screenshots: NOT_RUN;
  these reproducible artifacts do not substitute for owner review.
