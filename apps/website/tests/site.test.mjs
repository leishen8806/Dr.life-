import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const dist = fileURLToPath(new URL("../dist/", import.meta.url));
const source = (name) => readFileSync(fileURLToPath(new URL(`../src/${name}`, import.meta.url)), "utf8");

test("confirmed-design sections remain present in the source", () => {
  const home = source("pages/index.astro");
  const merchant = source("pages/merchant.astro");
  const legal = source("components/LegalPage.astro");

  for (const marker of ["hero-arc", "category-grid", "ecosystem-node", "chain-step"]) {
    assert.match(home, new RegExp(marker), `homepage is missing ${marker}`);
  }
  for (const marker of ["merchant-fee-table", "merchant-cta"]) {
    assert.match(merchant, new RegExp(marker), `merchant page is missing ${marker}`);
  }
  for (const marker of ["legal-history", "legal-note", "legal-current"]) {
    assert.match(legal, new RegExp(marker), `legal page is missing ${marker}`);
  }
});

test("warehouse scale and storage price use the board-approved values in every locale", () => {
  for (const locale of ["i18n/zh.json", "i18n/en.json", "i18n/km.json"]) {
    const copy = source(locale);
    assert.match(copy, /15,000 m³/, `${locale} must state the 15,000 m³ warehouse scale`);
    assert.match(copy, /15\s*(美元|ដុល្លារ)|US\$15/, `${locale} must state the US$15 storage price`);
    assert.doesNotMatch(copy, /10,000 m³/, `${locale} must not retain the former warehouse scale`);
    assert.doesNotMatch(copy, /14\s*(美元|ដុល្លារ)|US\$14/, `${locale} must not retain the former storage price`);
  }
});

test("confirmed home stage and ecosystem copy remains unabridged", () => {
  const copy = source("i18n/page-copy.ts");
  assert.match(copy, /家电、数码、日用、食品、美妆与更多品类。品牌旗舰店与本地商家并行/);
  assert.match(copy, /交易带来客户，仓配建立信任，结算保障资金，服务提升效率。每一项能力都为下一个品类复用。/);
});

test("legal page retains the confirmed table-of-contents label through i18n", () => {
  const legalPage = source("components/LegalPage.astro");
  const copy = source("i18n/page-copy.ts");
  assert.match(legalPage, /data-i18n="legal\.toc"/);
  assert.match(copy, /toc: "目录"/);
});

test("merchant delivery fee preserves the confirmed quote wording", () => {
  const copy = source("i18n/page-copy.ts");
  assert.match(copy, /配送费", "按订单实时报价", "买家支付，或商户设置包邮后由商户承担；下单时即显示金额。/);
});

test("merchant financial FAQ and payment-channel copy remain wired through i18n", () => {
  const merchant = source("pages/merchant.astro");
  const copy = source("i18n/page-copy.ts");
  assert.match(merchant, /merchant\.faqs/);
  assert.match(copy, /付款通道费", "\[以费用公告为准\]", "KESS 在线付款产生的通道费用，按公告的规则计算。/);
  assert.match(copy, /货款什么时候到账", "买家付款后款项先由平台托管；配送签收经复核，并过完 7 天售后观察期后，结算到商户钱包。提现由财务人工复核后出款。/);
});

test("merchant CTA retains the confirmed consultation path through i18n", () => {
  const merchant = source("pages/merchant.astro");
  const copy = source("i18n/page-copy.ts");
  assert.match(merchant, /merchant\.ctaTitle/);
  assert.match(merchant, /merchant\.contactCta/);
  assert.match(copy, /ctaTitle: "准备好了吗？"/);
});

test("merchant route uses the confirmed merchant-specific navigation through i18n", () => {
  const header = source("components/Header.astro");
  const copy = source("i18n/page-copy.ts");
  assert.match(header, /merchant\.nav/);
  assert.match(copy, /nav: \["商城", "商户入驻", "费用说明", "帮助与联系"\]/);
});

test("merchant hero preserves the confirmed title break and Bot header action through i18n", () => {
  const header = source("components/Header.astro");
  const merchant = source("pages/merchant.astro");
  const copy = source("i18n/page-copy.ts");
  assert.match(header, /merchant\.headerAction/);
  assert.match(merchant, /merchant\.titleLines/);
  assert.match(copy, /titleLines: \["在金边开一家线上建材店，", "不抽成"\]/);
});

test("home section headings preserve confirmed line breaks through i18n", () => {
  const home = source("pages/index.astro");
  assert.match(home, /home\.visionTitleLines/);
  assert.match(home, /home\.ecoTitleLines/);
  assert.match(home, /home\.chainTitleLines/);
});

test("the static build exposes the required public routes", () => {
  for (const page of ["index.html", "privacy/index.html", "terms/index.html"]) {
    assert.equal(existsSync(join(dist, page)), true, `${page} is missing`);
  }
});

test("the static build contains no prohibited claims and every internal link resolves", () => {
  const prohibited = ["24h 仓配无忧", "使命必达", "全柬配送", "一仓入柬、全柬销售", "精装交付与监理", "质量监理", "官方质检验收试点平台", "网商贷"];
  const htmlFiles = [];
  const collect = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) collect(path);
      if (entry.name.endsWith(".html")) htmlFiles.push(path);
    }
  };
  collect(dist);
  for (const file of htmlFiles) {
    const html = readFileSync(file, "utf8");
    for (const phrase of prohibited) assert.equal(html.includes(phrase), false, `${phrase} found in ${file}`);
    for (const href of html.matchAll(/href="(\/[^"]*)"/g)) {
      const [path] = href[1].split(/[?#]/);
      assert.equal(existsSync(join(dist, path, "index.html")) || existsSync(join(dist, path)), true, `${href[1]} from ${file} is unreachable`);
    }
  }
});

test("the static site loads a Khmer-capable font for Khmer pages", () => {
  const layout = source("components/Layout.astro");
  assert.match(layout, /Noto\+Sans\+Khmer/);
});
