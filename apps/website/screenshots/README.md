# PR #249 visual evidence

Website head: `0bf4cd0`; evidence refresh: pending

Full-page height check (static capture, Chinese): 1440px original `6998`,
implementation `6993` (0.07% difference); 390px original `9387`,
implementation `9439` (0.55% difference). Both are within the 5% acceptance
threshold. The capture command was
`pnpm --config.engine-strict=false --filter @drlife/website capture:screenshots`.

The generated matrix contains the six responsive widths in `manifest.txt` for
Chinese, English, and Khmer, plus section crops for the required 390px and
1440px review widths. Section crops are grouped under `sections/<width>/<lang>`
for the header, hero, business cards, route, app access, partners, and footer.

The original source references remain in `design/website/homepage-v2/` as
`reference-390.png` and `reference-1440.png`; review the corresponding new
full-page captures beside those references. Ready-to-review composites are in
`comparisons/390-zh-original-new.png` and
`comparisons/1440-zh-original-new.png`. The business-entry evidence is in
`sections/<width>/<lang>/business-tag-01.png` through `business-tag-04.png`.
The required per-section original|new composites for the Chinese 390px and
1440px review widths are in
`comparisons/sections/<width>/zh/<section>-original-new.png` for header, hero,
business, route, apps, partners, and footer.
The capture script validates the same geometry and overflow assertions used by
CI before writing the images. The mobile CTA capture also verifies that each
hero button stays within the viewport, the English actions may wrap without
horizontal overflow, while the Chinese 390px actions remain on one row; the
gold CTA arrow is explicitly rendered with a contrasting ink color. The two
full-page original|new comparisons were regenerated after this head.

Production Lighthouse is intentionally not claimed here: the repository's
`WEBSITE_PUBLIC_URL` secret is an external release condition. When configured,
`website-smoke.yml` runs the mobile preset and fails if LCP exceeds 2500ms.
