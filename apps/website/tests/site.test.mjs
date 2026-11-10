import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const dist = fileURLToPath(new URL("../dist/", import.meta.url));
const source = (name) => readFileSync(fileURLToPath(new URL(`../src/${name}`, import.meta.url)), "utf8");
const publicSource = (name) => readFileSync(fileURLToPath(new URL(`../public/${name}`, import.meta.url)), "utf8");

test("confirmed-design sections remain present in the source", () => {
  const home = source("pages/index.astro");
  const merchant = source("pages/merchant.astro");
  const legal = source("components/LegalPage.astro");

  for (const marker of ["hero-arc", "category-grid", "ecosystem-node", "chain-step"]) {
    assert.match(home, new RegExp(marker), `homepage is missing ${marker}`);
  }
  assert.match(home, /<picture>/, "category cards must use responsive picture markup");
  assert.match(home, /type="image\/avif"/, "category cards must expose AVIF");
  assert.match(home, /type="image\/webp"/, "category cards must expose WebP");
  assert.match(home, /data-i18n-alt={`home\.categoryAlts\.\$\{index\}`}/, "category cards must use localized alt keys");

  const imageCredits = publicSource("images/CREDITS.md");
  for (const photographer of ["Aspect 11", "R ARCHITECTURE", "Spacejoy", "Theme Photos"]) {
    assert.match(imageCredits, new RegExp(photographer.replace(" ", "\\s+")), `image credits missing ${photographer}`);
  }
  assert.equal((imageCredits.match(/https:\/\/unsplash\.com\/photos\//g) || []).length, 5, "each local image needs its original source page");
  for (const marker of ["merchant-fee-table", "merchant-cta"]) {
    assert.match(merchant, new RegExp(marker), `merchant page is missing ${marker}`);
  }
  for (const marker of ["legal-history", "legal-note", "legal-current"]) {
    assert.match(legal, new RegExp(marker), `legal page is missing ${marker}`);
  }
});

test("release output includes a localized, non-indexable 404 page with site icons", () => {
  const notFound = join(dist, "404.html");
  assert.ok(existsSync(notFound), "dist must include 404.html");

  const html = readFileSync(notFound, "utf8");
  assert.match(html, /name="robots" content="noindex"/, "404 page must not be indexed");
  assert.match(html, /rel="icon" href="\/favicon\.svg"/, "404 page must link the SVG favicon");
  assert.match(html, /rel="apple-touch-icon" href="\/apple-touch-icon\.png"/, "404 page must link the Apple touch icon");

  for (const asset of ["favicon.svg", "favicon.ico", "apple-touch-icon.png"]) {
    assert.ok(existsSync(join(dist, asset)), `dist must publish ${asset}`);
  }

  for (const file of readdirSync(dist, { recursive: true }).filter((entry) => entry.endsWith(".html"))) {
    const document = readFileSync(join(dist, file), "utf8");
    assert.match(document, /rel="icon" href="\/favicon\.svg"/, `${file} must link the SVG favicon`);
    assert.match(document, /rel="apple-touch-icon" href="\/apple-touch-icon\.png"/, `${file} must link the Apple touch icon`);
  }
});

test("404 copy is complete in Chinese, English and Khmer without placeholders", () => {
  const copy = source("i18n/not-found-copy.ts");
  const notFound = source("pages/404.astro");

  for (const locale of ["zh", "en", "km"]) {
    assert.match(copy, new RegExp(`${locale}:\\s*\\{[^}]+title:[^}]+intro:[^}]+home:[^}]+merchant:[^}]+contact:`, "s"));
  }
  assert.match(copy, /draft:/, "Khmer copy must disclose its draft status");
  assert.doesNotMatch(copy, /\[[^\]]+\]/, "404 copy must not contain bracket placeholders");
  const main = notFound.match(/<main class="not-found-page">([\s\S]*?)<\/main>/)?.[1] || "";
  assert.doesNotMatch(main, /\[[^\]]+\]/, "404 main content must not contain bracket placeholders");
});

test("release output uses self-hosted fonts only", () => {
  const files = readdirSync(dist, { recursive: true });
  const text = files
    .filter((file) => /\.(?:html|css)$/i.test(file))
    .map((file) => readFileSync(join(dist, file), "utf8"))
    .join("\n");

  assert.doesNotMatch(text, /(?:fonts\.googleapis\.com|fonts\.gstatic\.com|https?:\/\/[^\s"')]+\/[^\s"')]*\.(?:woff2?|ttf|otf))/i);
  assert.match(text, /\/fonts\//, "site CSS must reference locally published font files");
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

test("GROW WITH US cards preserve confirmed labels and partner copy without placeholders", () => {
  const home = source("pages/index.astro");
  const pageCopy = source("i18n/page-copy.ts");
  const partnerSection = home.slice(home.indexOf('<section class="section" id="about">'));
  const expected = {
    zh: [
      ["MERCHANTS", "本地商家入驻", "0% 销售佣金，仓储费 15 美元/立方米/月起，一张账单看清仓配费用。"],
      ["BRANDS · FACTORIES", "中国品牌与厂家", "不必自建海外仓：广州集货、跨境运输、金边入仓、线上销售一条龙。"],
      ["PARTNERS", "战略与生态伙伴", "物流、支付、金融机构与投资伙伴，共同建设柬埔寨的商业基础设施。"]
    ],
    en: [
      ["MERCHANTS", "Local merchant onboarding", "0% sales commission and storage from US$15/m³/month, with warehousing and fulfilment charges visible on one bill."],
      ["BRANDS · FACTORIES", "Chinese brands and manufacturers", "No need to build an overseas warehouse: Guangzhou consolidation, cross-border transport, Phnom Penh inbound warehousing and online sales in one flow."],
      ["PARTNERS", "Strategic and ecosystem partners", "Logistics, payment, financial institutions and investment partners are building Cambodia’s commercial infrastructure together."]
    ],
    km: [
      ["MERCHANTS", "ពាណិជ្ជករក្នុងស្រុក", "កម្រៃជើងសារលក់ 0%; ថ្លៃស្តុកចាប់ពី US$15/m³/ខែ; វិក្កយបត្រតែមួយបង្ហាញថ្លៃស្តុក និងដឹកជញ្ជូនច្បាស់។"],
      ["BRANDS · FACTORIES", "ម៉ាក និងរោងចក្រចិន", "មិនចាំបាច់សង់ឃ្លាំងក្រៅប្រទេស៖ ប្រមូលទំនិញនៅក្វាងចូវ ដឹកជញ្ជូនឆ្លងដែន ចូលឃ្លាំងភ្នំពេញ និងលក់តាមអនឡាញក្នុងសេវាតភ្ជាប់តែមួយ។"],
      ["PARTNERS", "ដៃគូយុទ្ធសាស្ត្រ និងប្រព័ន្ធអេកូឡូស៊ី", "ដៃគូផ្នែកភស្តុភារ ការទូទាត់ ស្ថាប័នហិរញ្ញវត្ថុ និងវិនិយោគ រួមគ្នាកសាងហេដ្ឋារចនាសម្ព័ន្ធពាណិជ្ជកម្មរបស់កម្ពុជា។"]
    ]
  };

  assert.match(partnerSection, /partners\.map\(\(\[label, title, body\], index\)/);
  assert.match(partnerSection, /home\.partners\.\$\{index\}\.0/);
  assert.match(partnerSection, /home\.partners\.\$\{index\}\.1/);
  assert.match(partnerSection, /home\.partners\.\$\{index\}\.2/);
  assert.doesNotMatch(partnerSection, /0\{index \+ 1\}/);

  for (const [locale, partners] of Object.entries(expected)) {
    const jsonCopy = JSON.parse(source(`i18n/${locale}.json`));
    assert.deepEqual(jsonCopy.home.partners, partners, `${locale}.json partners must match the confirmed copy`);
    for (const partner of jsonCopy.home.partners) assert.equal(partner.some((value) => value.startsWith("[")), false, `${locale}.json must not contain a partner placeholder`);
    for (const value of partners.flat()) assert.match(pageCopy, new RegExp(value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), `page-copy.ts must include ${locale} partner copy`);
  }

  assert.doesNotMatch(pageCopy, /\[(合作方向待确认|Cooperation direction to be confirmed|ទិសដៅសហការត្រូវបញ្ជាក់)\]/);
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
  const fonts = readFileSync(fileURLToPath(new URL("../public/fonts/fonts.css", import.meta.url)), "utf8");
  assert.match(layout, /noto-sans-khmer-.*\.woff2/, "Khmer font must be preloaded from this site");
  assert.match(fonts, /font-family:\s*'Noto Sans Khmer'/, "Khmer family must be self-hosted");
  assert.match(fonts, /font-display:\s*swap/, "Khmer glyphs must not be skipped when the font is late");
  assert.match(fonts, /unicode-range:/, "Khmer font must retain Unicode-range splits");
});

test("mobile Khmer display headings have an explicit no-clipping wrap guard", () => {
  const css = source("styles/global.css");
  assert.match(css, /\[data-lang="km"\] \.hero h1,\[data-lang="km"\] \.subtitle,\[data-lang="km"\] \.merchant-hero \.section-title,\[data-lang="km"\] \.legal-page h1\{max-inline-size:100%;min-width:0;overflow-wrap:anywhere;word-break:break-word;letter-spacing:normal\}/);
});

test("homepage service chain keeps its alignment and light-section eyebrow contrast", () => {
  const css = source("styles/global.css");
  assert.match(css, /\.chain\{margin:64px auto 0\}/);
  assert.match(css, /\.chain-note\{max-width:1280px;margin:32px auto 0\}/);
  assert.match(css, /\.section--light \.eyebrow,\.section:not\(\.section--dark\) \.eyebrow\{color:var\(--deep-gold\)\}/);
});
