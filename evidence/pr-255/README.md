# PR #255 evidence

- Code head: `2cdb73a` (latest fix commit on `feat/b14-seo-favicon`, based on merge `4c229ad`).
- Screenshot matrix: `screenshots/manifest.txt` and adjacent PNGs. The
  latest capture rerun completed with 78 screenshots, including 1440/390
  homepage and merchant routes for zh/en/km plus section captures.
- SEO metadata: [`curl-meta.txt`](curl-meta.txt), captured from the local
  static build for `/`, `/en/`, `/km/`, and `/merchant/`.
- Favicon assets: `assets/favicon.ico`, `assets/favicon.png`, and
  `assets/favicon.svg`; the small PNG/ICO assets are the copper-ring mark.

## Acceptance evidence

- Build/static output: PASS (the website build produced the static routes).
- SEO route metadata: PASS for the four routes listed in `curl-meta.txt`.
- Screenshot generation: PASS (the repository capture script completed after
  the canonical Chinese-language-link fix).
- Owner visual approval of favicon/logo and responsive screenshots: NOT_RUN;
  these reproducible artifacts do not substitute for owner review.
