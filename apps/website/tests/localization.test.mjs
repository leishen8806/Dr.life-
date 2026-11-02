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

  for (const key of ["home.stats", "home.stages", "home.ecosystem", "home.chain", "home.partners"]) {
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
