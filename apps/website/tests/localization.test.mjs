import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const page = (name) => readFileSync(fileURLToPath(new URL(`../src/pages/${name}.astro`, import.meta.url)), "utf8");
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

test("English entries for the localized content contain no Chinese characters", () => {
  for (const value of [
    ...english.home.stats.flat(),
    ...english.home.stages.flat(),
    ...english.home.ecosystem,
    ...english.home.chain,
    ...english.home.partners.flat(),
    english.merchant.fees[0][0],
    english.merchant.fees[1][0]
  ]) {
    assert.doesNotMatch(value, cjk, `${value} is not English`);
  }
});
