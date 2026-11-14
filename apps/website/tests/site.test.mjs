import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const dist = fileURLToPath(new URL("../dist/", import.meta.url));
const source = (name) =>
  readFileSync(
    fileURLToPath(new URL(`../src/${name}`, import.meta.url)),
    "utf8"
  );
const publicSource = (name) =>
  readFileSync(
    fileURLToPath(new URL(`../public/${name}`, import.meta.url)),
    "utf8"
  );

test("confirmed-design sections remain present in the source", () => {
  const home = source("pages/index.astro");
  const merchant = source("pages/merchant.astro");
  const legal = source("components/LegalPage.astro");

  for (const marker of [
    "hero-arc",
    "market-showcase",
    "market-group",
    "ecosystem-node",
    "chain-step"
  ]) {
    assert.match(home, new RegExp(marker), `homepage is missing ${marker}`);
  }
  assert.match(home, /<picture>/, "market visual must use responsive picture markup");
  assert.match(home, /type="image\/avif"/, "market visual must expose AVIF");
  assert.match(home, /type="image\/webp"/, "market visual must expose WebP");
  assert.match(home, /data-i18n-alt="home\.homepageV2\.market\.alts\.0"/, "market visual must use a localized alt key");

  const imageCredits = publicSource("images/CREDITS.md");
  for (const photographer of ["Aspect 11", "R ARCHITECTURE", "Spacejoy", "Theme Photos"]) {
    assert.match(imageCredits, new RegExp(photographer.replace(" ", "\\s+")), `image credits missing ${photographer}`);
  }
  assert.equal((imageCredits.match(/https:\/\/unsplash\.com\/photos\//g) || []).length, 5, "each local image needs its original source page");
  for (const url of imageCredits.match(/https:\/\/unsplash\.com\/photos\/[^\s)]+/g) || []) {
    assert.match(url, /^https:\/\/unsplash\.com\/photos\/[A-Za-z0-9][A-Za-z0-9-]+$/);
  }
  for (const marker of ["merchant-fee-table", "merchant-cta"]) {
    assert.match(
      merchant,
      new RegExp(marker),
      `merchant page is missing ${marker}`
    );
  }
  for (const marker of ["legal-history", "legal-note", "legal-current"]) {
    assert.match(legal, new RegExp(marker), `legal page is missing ${marker}`);
  }
});

test("published website images stay below the 200 KB budget", () => {
  for (const file of readdirSync(fileURLToPath(new URL("../public/images/", import.meta.url)))) {
    if (file === "CREDITS.md") continue;
    assert.ok(statSync(join(fileURLToPath(new URL("../public/images/", import.meta.url)), file)).size < 200 * 1024, `${file} exceeds 200 KB`);
  }
});

test("screenshot capture keeps traversal protection and static-resource failures", () => {
  const capture = readFileSync(fileURLToPath(new URL("../scripts/capture-screenshots.mjs", import.meta.url)), "utf8");
  assert.match(capture, /relative\(root, candidate\)/);
  assert.match(capture, /resourceResponse\.status\(\) >= 400/);
  assert.match(capture, /resourceType\(\) !== "document"/);
});

test("release output includes a localized, non-indexable 404 page with site icons", () => {
  const notFound = join(dist, "404.html");
  assert.ok(existsSync(notFound), "dist must include 404.html");

  const html = readFileSync(notFound, "utf8");
  assert.match(
    html,
    /name="robots" content="noindex"/,
    "404 page must not be indexed"
  );
  assert.match(
    html,
    /rel="icon" href="\/favicon\.svg"/,
    "404 page must link the SVG favicon"
  );
  assert.match(
    html,
    /rel="apple-touch-icon" href="\/apple-touch-icon\.png"/,
    "404 page must link the Apple touch icon"
  );

  for (const asset of ["favicon.svg", "favicon.ico", "apple-touch-icon.png"]) {
    assert.ok(existsSync(join(dist, asset)), `dist must publish ${asset}`);
  }

  for (const file of readdirSync(dist, { recursive: true }).filter((entry) =>
    entry.endsWith(".html")
  )) {
    const document = readFileSync(join(dist, file), "utf8");
    assert.match(
      document,
      /rel="icon" href="\/favicon\.svg"/,
      `${file} must link the SVG favicon`
    );
    assert.match(
      document,
      /rel="apple-touch-icon" href="\/apple-touch-icon\.png"/,
      `${file} must link the Apple touch icon`
    );
  }
});

test("404 copy is complete in Chinese, English and Khmer without placeholders", () => {
  const copy = source("i18n/not-found-copy.ts");
  const notFound = source("pages/404.astro");

  for (const locale of ["zh", "en", "km"]) {
    assert.match(
      copy,
      new RegExp(
        `${locale}:\\s*\\{[^}]+title:[^}]+intro:[^}]+home:[^}]+merchant:[^}]+contact:`,
        "s"
      )
    );
  }
  assert.match(copy, /draft:/, "Khmer copy must disclose its draft status");
  assert.doesNotMatch(
    copy,
    /\[[^\]]+\]/,
    "404 copy must not contain bracket placeholders"
  );
  const main =
    notFound.match(/<main class="not-found-page">([\s\S]*?)<\/main>/)?.[1] ||
    "";
  assert.doesNotMatch(
    main,
    /\[[^\]]+\]/,
    "404 main content must not contain bracket placeholders"
  );
});

test("release output uses self-hosted fonts only", () => {
  const files = readdirSync(dist, { recursive: true });
  const text = files
    .filter((file) => /\.(?:html|css)$/i.test(file))
    .map((file) => readFileSync(join(dist, file), "utf8"))
    .join("\n");

  assert.doesNotMatch(
    text,
    /(?:fonts\.googleapis\.com|fonts\.gstatic\.com|https?:\/\/[^\s"')]+\/[^\s"')]*\.(?:woff2?|ttf|otf))/i
  );
  assert.match(
    text,
    /\/fonts\//,
    "site CSS must reference locally published font files"
  );
});

test("warehouse scale and storage price use the board-approved values in every locale", () => {
  for (const locale of ["i18n/zh.json", "i18n/en.json", "i18n/km.json"]) {
    const copy = source(locale);
    assert.match(
      copy,
      /15,000 m³/,
      `${locale} must state the 15,000 m³ warehouse scale`
    );
    assert.match(
      copy,
      /15\s*(美元|ដុល្លារ)|US\$15/,
      `${locale} must state the US$15 storage price`
    );
    assert.doesNotMatch(
      copy,
      /10,000 m³/,
      `${locale} must not retain the former warehouse scale`
    );
    assert.doesNotMatch(
      copy,
      /14\s*(美元|ដុល្លារ)|US\$14/,
      `${locale} must not retain the former storage price`
    );
  }
});

test("confirmed home stage and ecosystem copy remains unabridged", () => {
  const copy = source("i18n/page-copy.ts");
  assert.match(
    copy,
    /家电、数码、日用、食品、美妆与更多品类。品牌旗舰店与本地商家并行/
  );
  assert.match(
    copy,
    /交易带来客户，仓配建立信任，结算保障资金，服务提升效率。每一项能力都为下一个品类复用。/
  );
});

test("legal page retains the confirmed table-of-contents label through i18n", () => {
  const legalPage = source("components/LegalPage.astro");
  const copy = source("i18n/page-copy.ts");
  assert.match(legalPage, /data-i18n="legal\.toc"/);
  assert.match(copy, /toc: "目录"/);
});

test("merchant delivery fee preserves the confirmed quote wording", () => {
  const copy = source("i18n/page-copy.ts");
  assert.match(
    copy,
    /配送费", "按订单实时报价", "买家支付，或商户设置包邮后由商户承担；下单时即显示金额。/
  );
});

test("merchant financial FAQ and payment-channel copy remain wired through i18n", () => {
  const merchant = source("pages/merchant.astro");
  const copy = source("i18n/page-copy.ts");
  assert.match(merchant, /merchant\.faqs/);
  assert.match(
    copy,
    /付款通道费", "\[以费用公告为准\]", "KESS 在线付款产生的通道费用，按公告的规则计算。/
  );
  assert.match(
    copy,
    /货款什么时候到账", "买家付款后款项先由平台托管；配送签收经复核，并过完 7 天售后观察期后，结算到商户钱包。提现由财务人工复核后出款。/
  );
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
  assert.match(home, /home\.homepageV2\.business\.titleLines/);
  assert.match(home, /home\.homepageV2\.route\.titleLines/);
});

test("GROW WITH US cards preserve confirmed labels and partner copy without placeholders", () => {
  const home = source("pages/index.astro");
  const pageCopy = source("i18n/page-copy.ts");
  assert.match(home, /partners\.map\(\(\[label, title, body\], index\)/);
  assert.match(home, /home\.homepageV2\.partners\.cards\.\$\{index\}\.0/);
  assert.match(home, /home\.homepageV2\.partners\.cards\.\$\{index\}\.1/);
  assert.match(home, /home\.homepageV2\.partners\.cards\.\$\{index\}\.2/);
  assert.match(pageCopy, /homepageV2Full/);
  assert.doesNotMatch(pageCopy, /\[(合作方向待确认|Cooperation direction to be confirmed|ទិសដៅសហការត្រូវបញ្ជាក់)\]/);
});

test("the static build exposes the required public routes", () => {
  for (const page of ["index.html", "privacy/index.html", "terms/index.html"]) {
    assert.equal(existsSync(join(dist, page)), true, `${page} is missing`);
  }
});

test("static locale pages publish the approved SEO metadata without a query string", () => {
  const html = (route) => readFileSync(join(dist, route || "index.html", route ? "index.html" : ""), "utf8");
  const pages = [
    ["", "zh-CN", "zh_CN", "DR.LIFE+ 生活家｜金边建材家居与中柬跨境物流平台 - DR集团"],
    ["en", "en", "en_US", "DR.LIFE+ | Building Materials &amp; Home Platform in Phnom Penh, Cambodia - DR Group"],
    ["km", "km", "km_KH", "DR.LIFE+ | វេទិកាសម្ភារៈសំណង់ និងគ្រឿងសង្ហារិមនៅភ្នំពេញ - DR Group"]
  ];
  for (const [route, lang, locale, title] of pages) {
    const source = html(route);
    assert.match(source, new RegExp(`<html lang="${lang}"`));
    assert.ok(source.includes(`<title>${title}</title>`), `${route || "zh"} title changed`);
    assert.ok(source.includes(`property="og:locale" content="${locale}"`), `${route || "zh"} locale changed`);
    assert.match(source, /name="keywords" content="[^"]+/);
    assert.doesNotMatch(source, /\?lang=/);
  }
  for (const route of ["merchant", "en/merchant", "km/merchant"]) {
    assert.match(html(route), /name="keywords" content="[^"]+/);
    assert.match(html(route), /property="og:image" content="https:\/\/drlifeplus\.com\/og-image\.jpg"/);
  }
});

test("static locale pages keep the Chinese language switch on the canonical Chinese route", () => {
  const html = (route) => readFileSync(join(dist, route, "index.html"), "utf8");
  assert.match(html("en"), /<a href="\/" data-language="zh">中文<\/a>/);
  assert.match(html("km/merchant"), /<a href="\/merchant\/" data-language="zh">中文<\/a>/);
});

test("the static build contains no prohibited claims and every internal link resolves", () => {
  const prohibited = [
    "24h 仓配无忧",
    "使命必达",
    "全柬配送",
    "一仓入柬、全柬销售",
    "精装交付与监理",
    "质量监理",
    "官方质检验收试点平台",
    "网商贷"
  ];
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
    for (const phrase of prohibited)
      assert.equal(html.includes(phrase), false, `${phrase} found in ${file}`);
    for (const href of html.matchAll(/href="(\/[^"]*)"/g)) {
      const [path] = href[1].split(/[?#]/);
      assert.equal(
        existsSync(join(dist, path, "index.html")) ||
          existsSync(join(dist, path)),
        true,
        `${href[1]} from ${file} is unreachable`
      );
    }
  }
});

test("the static site loads a Khmer-capable font for Khmer pages", () => {
  const layout = source("components/Layout.astro");
  const fonts = readFileSync(
    fileURLToPath(new URL("../public/fonts/fonts.css", import.meta.url)),
    "utf8"
  );
  assert.match(
    layout,
    /noto-sans-khmer-.*\.woff2/,
    "Khmer font must be preloaded from this site"
  );
  assert.match(
    fonts,
    /font-family:\s*'Noto Sans Khmer'/,
    "Khmer family must be self-hosted"
  );
  assert.match(
    fonts,
    /font-display:\s*swap/,
    "Khmer glyphs must not be skipped when the font is late"
  );
  assert.match(
    fonts,
    /unicode-range:/,
    "Khmer font must retain Unicode-range splits"
  );
});

test("mobile Khmer display headings have an explicit no-clipping wrap guard", () => {
  const css = source("styles/global.css");
  assert.match(
    css,
    /\[data-lang=\s*["']?km["']?\s*\][^}]*max-inline-size:\s*100%;[^}]*overflow-wrap:\s*anywhere;[^}]*word-break:\s*break-word;[^}]*letter-spacing:\s*normal/
  );
  assert.match(
    css,
    /\[data-lang="km"\] \.hero h1,[\s\S]*?white-space:\s*normal/,
    "Khmer hero headings must override the global nowrap rule"
  );
  assert.match(css, /\.stat span\s*\{[^}]*overflow-wrap:\s*anywhere;/);
  assert.doesNotMatch(css, /\.stat span\s*\{[^}]*white-space:\s*nowrap/);
  assert.match(css, /\.stat strong\s*\{[^}]*word-break:\s*keep-all/);
  assert.match(css, /\.stat strong\s*\{[^}]*white-space:\s*nowrap/);
  assert.doesNotMatch(css, /\.hero h1\s*\{[^}]*white-space:\s*nowrap/);
  assert.doesNotMatch(css, /\.subtitle\s*\{[^}]*white-space:\s*nowrap/);
});

test("not-found draft copy uses selector specificity instead of !important", () => {
  const css = source("styles/global.css");
  assert.match(css, /\.not-found-page > p\.not-found-page__draft\s*\{[^}]*font-size:\s*var\(--fs-small\);/);
  assert.doesNotMatch(css, /\.not-found-page__draft\s*\{[^}]*!important/);
  assert.equal((css.match(/\[data-lang="km"\] \.not-found-page h1\s*\{/g) ?? []).length, 0);
});

test("homepage service chain keeps its alignment and light-section eyebrow contrast", () => {
  const css = source("styles/global.css");
  assert.match(css, /\.chain\s*\{[^}]*margin:\s*64px auto 0/);
  assert.match(
    css,
    /\.chain-note\s*\{[^}]*max-width:\s*1280px[^}]*margin:\s*32px auto 0/
  );
  assert.match(
    css,
    /\.section--light \.eyebrow,\s*\.section:not\(\.section--dark\) \.eyebrow\s*\{\s*color:\s*var\(--gold-text\)/
  );
});

test("responsive type uses the approved scale outside the root declaration", () => {
  const css = source("styles/global.css");
  for (const variable of [
    "--fs-display",
    "--fs-tagline",
    "--fs-h2",
    "--fs-h3",
    "--fs-stat",
    "--fs-lead",
    "--fs-body",
    "--fs-small",
    "--fs-eyebrow"
  ]) {
    assert.match(
      css,
      new RegExp(`${variable}:`),
      `${variable} must be defined`
    );
  }

  const rootEnd = css.indexOf("}");
  const outsideRoot = rootEnd === -1 ? css : css.slice(rootEnd + 1);
  assert.doesNotMatch(
    outsideRoot,
    /font-size\s*:\s*(?!var\(--fs-)[^;}]*\d+px/,
    "font sizes must use the approved variables"
  );
  assert.match(
    css,
    /\.section--light \.eyebrow,\s*\.section:not\(\.section--dark\) \.eyebrow\s*\{\s*color:\s*var\(--gold-text\)/
  );
});
