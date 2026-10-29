# Catalog sources

Fetched/checked: 2026-10-29. This dataset contains only category names and
generic specification labels. It contains no product pages, images, prices,
merchant/store data, or copied product descriptions.

| Source | Accessed | Use in this dataset | Terms/licence note | Method |
| --- | --- | --- | --- | --- |
| https://www.google.com/basepages/producttype/taxonomy-with-ids.en-US.txt | 2026-10-29 | Category-tree reference (`GPC`) | Google Merchant Center documentation identifies this as the Google Product Taxonomy; use the current official list when mapping a listing. No file was downloaded or committed. Google Terms say these establish what is expected when using Google services. | Browser search and official documentation review |
| https://support.google.com/merchants/answer/7052112 | 2026-10-29 | `color`, `material`, `size`, and factual product-detail label conventions (`Google-spec`) | Google help documentation; consulted as reference, not reproduced. Short excerpt: “Value from the Google product taxonomy.” | Browser review |
| https://developers.google.com/search/docs/appearance/structured-data/merchant-listing?hl=zh-cn | 2026-10-29 | Verifies that a Google category ID/path points to the public taxonomy | Google developer documentation; reference only. | Browser review |
| https://www.ikea.com/kr/en/cat/products-products/ | 2026-10-29 | Public navigation labels for kitchen, bathroom, lighting and storage (`IKEA-nav`) | Public navigational labels viewed without login; no product, image, price, or store data collected. IKEA's terms say its conditions “may change from time to time”; re-check before any automation. | Browser review |
| `prototype/index.html` and `Dr.life+交互文档.md` | local | Ensures trial categories include cabinets, flooring, bathroom, tiles, doors/windows, furniture, basic materials and paint (`prototype`) | Project-owned design references. | Local read-only review |

## Usage boundaries

- `id` values here are Dr.life+ stable catalog IDs, not Google taxonomy IDs.
- Khmer strings are machine-translation drafts and each is explicitly marked `（待审）`.
  Localizer must approve or replace them before customer-facing publication.
- This is a taxonomy/template draft, not an import feed or a claim that a named
  marketplace permits crawling. Refreshes must re-check robots.txt and terms;
  do not log in, bypass controls, or collect product-detail, image, price, or
  merchant information.
