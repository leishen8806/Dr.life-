# PR #249 visual evidence

Head: `17b14b8`

The generated matrix contains the six responsive widths in `manifest.txt` for
Chinese, English, and Khmer, plus section crops for the required 390px and
1440px review widths. Section crops are grouped under `sections/<width>/<lang>`
for the header, hero, business cards, route, app access, partners, and footer.

The original source references remain in `design/website/homepage-v2/` as
`reference-390.png` and `reference-1440.png`; review the corresponding new
full-page captures beside those references. The capture script validates the
same geometry and overflow assertions used by CI before writing the images.

Production Lighthouse is intentionally not claimed here: the repository's
`WEBSITE_PUBLIC_URL` secret is an external release condition. When configured,
`website-smoke.yml` runs the mobile preset and fails if LCP exceeds 2500ms.
