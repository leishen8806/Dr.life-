import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const page = (name) => readFileSync(fileURLToPath(new URL(`../src/pages/${name}.astro`, import.meta.url)), "utf8");
const styles = readFileSync(fileURLToPath(new URL("../src/styles/global.css", import.meta.url)), "utf8");
const english = JSON.parse(readFileSync(fileURLToPath(new URL("../src/i18n/en.json", import.meta.url)), "utf8"));
const cjk = /[\u3400-\u9fff]/;

test("English home content paths are wired for every repeated buyer-visible section", () => {
  const source = page("index");

  for (const key of ["home.homepageV2.business.cards", "home.homepageV2.market.groups", "home.homepageV2.route.steps", "home.homepageV2.culture.values", "home.homepageV2.partners.cards", "home.homepageV2.appsV2.cards"]) {
    assert.match(source, new RegExp("data-i18n=\\{`" + key + "\\.\\$\\{index\\}"), `${key} is not localized`);
  }
});

test("merchant commission and settlement labels are i18n-backed", () => {
  const source = page("merchant");

  assert.match(source, /data-i18n="merchant\.fees\.0\.0"/, "commission label is not localized");
  assert.match(source, /data-i18n="merchant\.fees\.1\.0"/, "settlement label is not localized");
  assert.doesNotMatch(source, cjk, "merchant page retains a Chinese hard-coded label");
});

test("merchant step and fee cards are i18n-backed field by field", () => {
  const source = page("merchant");

  for (const key of ["merchant.steps", "merchant.fees"]) {
    for (const field of [0, 1]) {
      assert.match(
        source,
        new RegExp("data-i18n=\\{`" + key + "\\.\\$\\{index\\}\\." + field + "`\\}"),
        `${key}.${field} is not localized`
      );
    }
  }
  assert.match(source, /data-i18n=\{`merchant\.fees\.\$\{index\}\.2`\}/, "merchant.fees.2 is not localized");
});

test("English entries for the localized content contain no Chinese characters", () => {
  for (const value of [
    ...english.home.stats.flat(),
    ...english.home.stages.flat(),
    ...english.home.ecosystem,
    ...english.home.chain,
    ...english.home.partners.flat(),
    ...english.merchant.steps.flat(),
    ...english.merchant.fees.flat(),
    english.merchant.fees[0][0],
    english.merchant.fees[1][0]
  ]) {
    assert.doesNotMatch(value, cjk, `${value} is not English`);
  }
});

test("mobile English headings and supporting copy have a wrapping safeguard", () => {
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*520px\s*\)[\s\S]*?\.eyebrow[\s\S]*?overflow-wrap:\s*anywhere\s*;/
  );
  assert.match(
    styles,
    /@media\s*\(\s*min-width:\s*521px\s*\)\s*and\s*\(\s*max-width:\s*600px\s*\)[\s\S]*?\[data-lang="km"\] \.hero h1[\s\S]*?overflow-wrap:\s*anywhere\s*;/
  );
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*520px\s*\)[\s\S]*?\.merchant-hero \.section-title[\s\S]*?overflow-wrap:\s*anywhere\s*;/
  );
});

test("responsive typography balances headings and keeps the locale eyebrow hyphen unbroken", () => {
  assert.match(styles, /h1,\s*h2,\s*h3,\s*\.eyebrow\s*\{[\s\S]*?text-wrap:\s*balance\s*;/);
  assert.match(styles, /body\s*\{[\s\S]*?text-wrap:\s*pretty\s*;/);

  for (const name of ["zh", "en", "km"]) {
    const locale = readFileSync(fileURLToPath(new URL(`../src/i18n/${name}.json`, import.meta.url)), "utf8");
    assert.match(locale, /NEXT‑GENERATION/);
    assert.doesNotMatch(locale, /NEXT-GENERATION/);
  }
});

test("base typography uses the shared body-size variable", () => {
  assert.match(styles, /:root\s*\{[\s\S]*?--fs-body:[^;]+;[\s\S]*?font-size:\s*var\(--fs-body\);/);
});

test("Khmer typography keeps readable line heights at every width", () => {
  assert.match(styles, /\[data-lang="km"\]\s*\{\s*line-height:\s*1\.85;/);
  assert.match(styles, /\[data-lang="km"\] \.hero h1,[\s\S]*?line-height:\s*1\.35;/);
  assert.match(styles, /@media\s*\(min-width:\s*521px\) and \(max-width:\s*900px\)/);
});

test("base and mobile hero line heights match the typography specification", () => {
  assert.match(styles, /:root\s*\{[\s\S]*?line-height:\s*1\.7;/);
  assert.match(styles, /@media\s*\(max-width:\s*520px\)[\s\S]*?\.hero h1\s*\{[\s\S]*?line-height:\s*1\.15;/);
});

test("mobile stats keep the displayed number and unit together", () => {
  assert.match(styles, /\.stat strong\s*\{[\s\S]*?word-break:\s*keep-all;/);
});

test("confirmed mobile layout keeps hero stats and ecosystem nodes compact", () => {
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*520px\s*\)[\s\S]*?\.stats\s*\{[\s\S]*?grid-template-columns:\s*repeat\(4,\s*minmax\(0,\s*1fr\)\)/
  );
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*520px\s*\)[\s\S]*?\.ecosystem-orbit\s*\{[\s\S]*?height:\s*auto;[\s\S]*?display:\s*grid;[\s\S]*?grid-template-columns:\s*repeat\(2,\s*1fr\)/
  );
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*520px\s*\)[\s\S]*?\.hero h1\s*\{[\s\S]*?font-size:\s*var\(--fs-display\)/
  );
});

test("320px stats use a two-column fallback and can wrap long numbers", () => {
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*360px\s*\)[\s\S]*?\.stats\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)[\s\S]*?\.stat strong[\s\S]*?white-space:\s*normal/
  );
});

test("English and Khmer homepage stats use the same three-language count", () => {
  const copy = readFileSync(fileURLToPath(new URL("../src/i18n/page-copy.ts", import.meta.url)), "utf8");
  assert.match(copy, /stats: \[\["0%"[\s\S]*?\["3\+", "Chinese · English · Khmer"\]\]/);
  assert.match(copy, /stats: \[\["0%"[\s\S]*?\["3\+", "中文 · English · ខ្មែរ"\]\]/);
});

test("mobile language links stay on one line beside the menu", () => {
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*520px\s*\)[\s\S]*?\.languages a\s*\{[\s\S]*?padding:\s*10px 6px;[\s\S]*?white-space:\s*nowrap[\s\S]*?\.mobile-menu\s*\{[\s\S]*?margin-left:\s*0/
  );
});

test("320px header moves language selection into the accessible hamburger menu", () => {
  const header = readFileSync(fileURLToPath(new URL("../src/components/Header.astro", import.meta.url)), "utf8");

  assert.match(header, /class="mobile-menu-languages"/);
  assert.match(header, /data-language="zh"/);
  assert.match(header, /data-language="en"/);
  assert.match(header, /data-language="km"/);
  assert.match(
    styles,
    /@media\s*\(\s*max-width:\s*360px\s*\)[\s\S]*?\.header-actions \.languages\s*\{[\s\S]*?display:\s*none[\s\S]*?\.mobile-menu-languages\s*\{[\s\S]*?display:\s*flex/
  );
  assert.match(
    styles,
    /@media\s*\(\s*min-width:\s*361px\s*\)[\s\S]*?\.mobile-menu-languages\s*\{[\s\S]*?display:\s*none/
  );
  assert.match(
    styles,
    /\.mobile-menu summary:focus-visible\s*\{[\s\S]*?outline:\s*3px solid var\(--gold\)[\s\S]*?outline-offset:\s*4px/
  );
});

test("website navigation has keyboard-visible focus and images are explicitly absent or accessible", () => {
  const pages = ["index", "merchant", "privacy", "terms", "contact", "delete-account", "404"]
    .map((name) => readFileSync(fileURLToPath(new URL(`../src/pages/${name}.astro`, import.meta.url)), "utf8"))
    .join("\n");

  assert.match(
    styles,
    /a:focus-visible\s*\{[\s\S]*?outline:\s*3px solid var\(--gold\)[\s\S]*?outline-offset:\s*4px/
  );
  assert.doesNotMatch(pages, /<img(?![^>]*\balt=)/, "any image must have an alt attribute");
});

test("homepage category images have localized alt text and local responsive sources", () => {
  const source = page("index");
  const localizer = readFileSync(fileURLToPath(new URL("../src/components/Localizer.astro", import.meta.url)), "utf8");
  assert.match(source, /data-i18n-alt=\{`home\.categoryAlts\.\$\{index\}`\}/);
  assert.match(source, /type="image\/avif"/);
  assert.match(source, /type="image\/webp"/);
  assert.match(source, /loading="lazy"/);
  assert.match(source, /srcset=/);
  assert.match(localizer, /data-i18n-alt/);
});

test("homepage and legal visible copy are i18n-backed", () => {
  for (const name of ["index", "merchant"]) {
    assert.doesNotMatch(page(name), cjk, `${name} page retains Chinese hard-coded copy`);
  }
  const legal = readFileSync(fileURLToPath(new URL("../src/components/LegalPage.astro", import.meta.url)), "utf8");
  assert.doesNotMatch(legal, cjk, "legal page retains Chinese hard-coded copy");
});

test("homepage app access uses dedicated trilingual copy and working anchors", () => {
  const home = page("index");
  const copy = readFileSync(fileURLToPath(new URL("../src/i18n/page-copy.ts", import.meta.url)), "utf8");
  for (const key of ["appsLabel", "appsTitle", "appsIntro", "appMarketplaceTitle", "appMarketplaceBody", "appMerchantTitle", "appMerchantBody"])
    assert.equal((copy.match(new RegExp(`\\b${key}:`, "g")) ?? []).length, 3, `${key} must be present in zh/en/km`);
  assert.match(home, /id="about"/);
  assert.doesNotMatch(home, /data-copy-channel/);
  assert.doesNotMatch(styles, /\.business-panels|\.category-groups|\.panel-list/);
  assert.doesNotMatch(styles, /#c9c1b4|#4b4f53|#57534d|#b7b5b0|font-family: "Noto Serif SC"/);
});

test("confirmed-page Khmer additions do not fall back to Chinese", () => {
  const copy = readFileSync(fileURLToPath(new URL("../src/i18n/page-copy.ts", import.meta.url)), "utf8");

  assert.doesNotMatch(copy, /const km = zh/, "Khmer confirmed-page copy must not fall back to Chinese");
});
