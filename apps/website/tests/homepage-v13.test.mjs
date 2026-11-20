import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const source = (relativePath) => readFileSync(
  fileURLToPath(new URL(`../${relativePath}`, import.meta.url)),
  "utf8"
);

test("homepage partner layout keeps five cards balanced across breakpoints", () => {
  const styles = source("src/styles/homepage-source.css");

  assert.match(styles, /\.partner-grid\s*\{[^}]*grid-template-columns:repeat\(6,minmax\(0,1fr\)\)/);
  assert.match(styles, /\.partner-grid \.partner:nth-child\(-n\+3\)\s*\{\s*grid-column:\s*span 2;/);
  assert.match(styles, /\.partner-grid \.partner:nth-child\(n\+4\)\s*\{\s*grid-column:\s*span 3;/);
  assert.match(styles, /@media\(max-width:760px\)[\s\S]*?\.partner-grid\{grid-template-columns:repeat\(2,minmax\(0,1fr\)\)/);
  assert.match(styles, /@media\(max-width:520px\)[\s\S]*?\.partner-grid\{grid-template-columns:1fr\}/);
});

test("homepage partner cards keep four preparation links and localized copy", () => {
  const html = source("src/data/homepage-source.html");
  const translations = source("public/scripts/homepage-source.js");
  const preparationLabels = ["准备采购需求", "准备入柬资料", "准备合作资料", "准备仓配需求"];

  assert.equal((html.match(/class="partner"/g) ?? []).length, 5);
  for (const label of preparationLabels) {
    assert.match(html, new RegExp(`href="/contact">${label}`));
    assert.match(translations, new RegExp(label));
  }
});

test("homepage screenshot assertions guard the menu viewport and contact flow", () => {
  const capture = source("scripts/capture-screenshots.mjs");

  assert.match(capture, /menuToggleBox\.x \+ menuToggleBox\.width > width/);
  assert.match(capture, /mainLinks = openNav\.locator/);
  assert.match(capture, /partnerLink\.getAttribute\("href"\).*endsWith\("\/contact"\)/);
  assert.match(capture, /const widths = \[360, 390, 640, 768, 1280, 1440, 1920\]/);
  assert.match(capture, /const languages = \["zh", "en", "km"\]/);
});
