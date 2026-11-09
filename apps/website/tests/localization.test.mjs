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

  for (const key of ["home.stats", "home.stages", "home.ecosystemNodes", "home.chain", "home.partners"]) {
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
  assert.match(styles, /@media\(max-width:520px\).*\.eyebrow.*overflow-wrap:anywhere/s);
  assert.match(styles, /@media\(max-width:520px\).*\.merchant-hero \.section-title.*overflow-wrap:anywhere/s);
});

test("confirmed mobile layout keeps hero stats and ecosystem nodes compact", () => {
  assert.match(styles, /@media\(max-width:520px\).*\.stats\{grid-template-columns:repeat\(4,1fr\)/s);
  assert.match(styles, /@media\(max-width:520px\).*\.ecosystem-orbit\{height:auto;display:grid;grid-template-columns:repeat\(2,1fr\)/s);
  assert.match(styles, /@media\(max-width:520px\).*\.hero h1\{font-size:60px/s);
});

test("mobile language links stay on one line beside the menu", () => {
  assert.match(styles, /@media\(max-width:520px\).*\.languages a\{padding:10px 6px;white-space:nowrap\}.*\.mobile-menu\{margin-left:0\}/s);
});

test("320px header moves language selection into the accessible hamburger menu", () => {
  const header = readFileSync(fileURLToPath(new URL("../src/components/Header.astro", import.meta.url)), "utf8");

  assert.match(header, /class="mobile-menu-languages"/);
  assert.match(header, /data-language="zh"/);
  assert.match(header, /data-language="en"/);
  assert.match(header, /data-language="km"/);
  assert.match(styles, /@media\(max-width:360px\)\{\.header-actions \.languages\{display:none\}\.mobile-menu-languages\{display:flex\}\}/);
  assert.match(styles, /@media\(min-width:361px\)\{\.mobile-menu-languages\{display:none\}\}/);
  assert.match(styles, /\.mobile-menu summary:focus-visible\{outline:3px solid var\(--gold\);outline-offset:4px\}/);
});

test("website navigation has keyboard-visible focus and images are explicitly absent or accessible", () => {
  const pages = ["index", "merchant", "privacy", "terms", "contact", "delete-account", "404"]
    .map((name) => readFileSync(fileURLToPath(new URL(`../src/pages/${name}.astro`, import.meta.url)), "utf8"))
    .join("\n");

  assert.match(styles, /a:focus-visible\{outline:3px solid var\(--gold\);outline-offset:4px\}/);
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

test("confirmed-page Khmer additions do not fall back to Chinese", () => {
  const copy = readFileSync(fileURLToPath(new URL("../src/i18n/page-copy.ts", import.meta.url)), "utf8");

  assert.doesNotMatch(copy, /const km = zh/, "Khmer confirmed-page copy must not fall back to Chinese");
});
