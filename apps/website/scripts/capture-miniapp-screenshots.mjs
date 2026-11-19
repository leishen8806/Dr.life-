import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const output = fileURLToPath(new URL("../screenshots-miniapp/", import.meta.url));
const matrix = [390, 1440].flatMap((width) =>
  ["light", "dark"].flatMap((theme) =>
    ["zh", "en", "km"].map((language) => ({ width, theme, language }))
  )
);

const launchParams = (theme, language) => {
  const data = encodeURIComponent(`user=${encodeURIComponent(JSON.stringify({ id: 1, language_code: language }))}`);
  const themeParams = encodeURIComponent(JSON.stringify({
    bg_color: theme === "dark" ? "#111315" : "#f4f6f8",
    text_color: theme === "dark" ? "#f4f6f8" : "#111315",
    color_scheme: theme
  }));
  return `?tgWebAppPlatform=tdesktop&tgWebAppVersion=7.0&tgWebAppData=${data}&tgWebAppThemeParams=${themeParams}`;
};

const start = (app, port) => {
  const vite = join(root, "node_modules", ".bin", "vite");
  const child = spawn(vite, ["--host", "127.0.0.1", "--port", String(port), "--strictPort"], {
    cwd: join(root, "apps", app),
    stdio: "inherit"
  });
  return { child, port };
};

const waitFor = async (port) => {
  for (let attempt = 0; attempt < 240; attempt += 1) {
    try {
      const response = await fetch(`http://127.0.0.1:${port}`, { signal: AbortSignal.timeout(1000) });
      if (response.ok) return;
    } catch { /* Vite is still starting. */ }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`Mini App dev server did not start on ${port}`);
};

const json = (data) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ data })
});

async function installMockApi(page, language, app) {
  const merchantOrder = {
    public_id: "01EVIDENCEMERCHANTORDER",
    parent_public_id: "01EVIDENCEORDER",
    status: "shipped",
    items: [{ sku_public_id: "SKU-1", quantity: 2, unit_price_e4: 95000, line_total_e4: 190000 }],
    shipping_address_public_id: "01EVIDENCEADDRESS",
    shipping_address: { recipient_name: "Buyer", address_text: "Phnom Penh", phone: "****1234" },
    goods_amount_e4: 190000,
    shipping_amount_e4: 0,
    payable_amount_e4: 190000,
    shipping_quote_expires_at: "2026-11-15T10:00:00Z",
    created_at: "2026-11-14T09:00:00Z",
    updated_at: "2026-11-14T09:01:00Z"
  };
  await page.route("**/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (path === "/v1/public/auth/telegram" && request.method() === "POST") {
      await route.fulfill(json({
        access_token: "evidence-access-token",
        refresh_token: "evidence-refresh-token",
        account: {
          public_id: `01EVIDENCE${app.toUpperCase()}`,
          role: app,
          lang: language,
          status: "active",
          needs_language: false
        }
      }));
      return;
    }
    if (path === "/v1/public/auth/refresh" && request.method() === "POST") {
      await route.fulfill(json({ access_token: "evidence-access-token", refresh_token: "evidence-refresh-token" }));
      return;
    }
    if (path.endsWith("/me/consents") && request.method() === "GET") {
      await route.fulfill(json({ items: [] }));
      return;
    }
    if (path === "/v1/public/categories" && request.method() === "GET") {
      await route.fulfill(json({ items: [{ public_id: "01EVIDENCECATEGORY", code: "building", name: { zh: "建材", en: "Building materials", km: "សម្ភារៈសំណង់" }, enabled: true, sort: 1 }], total: 1, page: 1, page_size: 100 }));
      return;
    }
    if (path === "/v1/public/products" && request.method() === "GET") {
      await route.fulfill(json({ items: [
        { public_id: "01EVIDENCEPRODUCT1", category_code: "building", name: { zh: "瓷砖样品", en: "Tile sample", km: "គំរូក្បឿង" }, spec: { zh: "600×600 mm", en: "600×600 mm", km: "600×600 មម" }, unit: "m²", origin: "cn", lead_days: 14, status: "on", source: "manual", tiers: [{ min_qty: 1, unit_price_e4: 125000 }], skus: [], updated_at: "2026-01-01T00:00:00Z" },
        { public_id: "01EVIDENCEPRODUCT2", category_code: "building", name: { zh: "室内门", en: "Interior door", km: "ទ្វារខាងក្នុង" }, spec: { zh: "实木复合", en: "Engineered wood", km: "ឈើផ្សំ" }, unit: "piece", origin: "cn", lead_days: 21, status: "on", source: "manual", tiers: [{ min_qty: 1, unit_price_e4: 890000 }], skus: [], updated_at: "2026-01-01T00:00:00Z" }
      ], total: 2, page: 1, page_size: 20 }));
      return;
    }
    if (path === "/v1/buyer/orders" && request.method() === "GET") {
      await route.fulfill(json({ items: [{
        public_id: "01EVIDENCEORDER",
        payment_status: "succeeded",
        summary_status: "paid",
        goods_amount_e4: 190000,
        shipping_amount_e4: 0,
        payable_amount_e4: 190000,
        created_at: "2026-11-14T09:00:00Z",
        suborders: [{
          public_id: "01EVIDENCESUBORDER",
          merchant_public_id: "01EVIDENCEMERCHANT",
          status: "paid",
          items: [{ sku_public_id: "01EVIDENCESKU", quantity: 2, unit_price_e4: 95000, line_total_e4: 190000 }],
          goods_amount_e4: 190000,
          shipping_amount_e4: 0,
          payable_amount_e4: 190000,
          shipping_quote_expires_at: "2026-11-15T10:00:00Z",
          events: [{ status: "paid", occurred_at: "2026-11-14T09:01:00Z" }, { status: "submitted", occurred_at: "2026-11-14T09:02:00Z" }]
        }]
      }], next_cursor: null }));
      return;
    }
    if (path === "/v1/buyer/orders/01EVIDENCEORDER" && request.method() === "GET") {
      await route.fulfill(json({
        public_id: "01EVIDENCEORDER",
        payment_status: "succeeded",
        summary_status: "paid",
        goods_amount_e4: 190000,
        shipping_amount_e4: 0,
        payable_amount_e4: 190000,
        created_at: "2026-11-14T09:00:00Z",
        suborders: [{ public_id: "01EVIDENCESUBORDER", merchant_public_id: "01EVIDENCEMERCHANT", status: "paid", items: [{ sku_public_id: "01EVIDENCESKU", quantity: 2, unit_price_e4: 95000, line_total_e4: 190000 }], goods_amount_e4: 190000, shipping_amount_e4: 0, payable_amount_e4: 190000, shipping_quote_expires_at: "2026-11-15T10:00:00Z", events: [{ status: "paid", occurred_at: "2026-11-14T09:01:00Z" }, { status: "submitted", occurred_at: "2026-11-14T09:02:00Z" }] }]
      }));
      return;
    }
    if (path === "/v1/merchant/products" && request.method() === "GET") {
      await route.fulfill(json({ items: [] }));
      return;
    }
    if (path === "/v1/merchant/suborders" && request.method() === "GET") {
      await route.fulfill(json({ items: [merchantOrder], next_cursor: null }));
      return;
    }
    if (path === "/v1/merchant/suborders/01evidencemerchantorder" && request.method() === "GET") {
      await route.fulfill(json(merchantOrder));
      return;
    }
    if (path.endsWith("/me/notifications") && request.method() === "GET") {
      await route.fulfill(json({ items: [{ public_id: "01EVIDENCENOTIFICATION", event_id: "evt-1", event_type: "order.created", title: { zh: "订单通知", en: "Order notification", km: "ការជូនដំណឹងការបញ្ជាទិញ" }, body: { zh: "订单已创建", en: "Your order was created", km: "ការបញ្ជាទិញរបស់អ្នកត្រូវបានបង្កើត" }, read_at: null, created_at: "2026-01-01T00:00:00Z" }], unread_count: 1 }));
      return;
    }
    if (path.endsWith("/me/notification-settings") && request.method() === "GET") {
      await route.fulfill(json({ items: [] }));
      return;
    }
    if (path.includes("/me/notifications/") || path.endsWith("/me/notifications/read-all")) {
      await route.fulfill(json({ updated: true }));
      return;
    }
    if (path.endsWith("/me/notification-settings") && request.method() === "PUT") {
      await route.fulfill(json({ items: [{ event_type: "order.created", in_app_enabled: true, telegram_enabled: true }] }));
      return;
    }
    await route.continue();
  });
}

async function waitForBusinessPage(page, app) {
  const splash = page.locator(".drl-splash__skip");
  if (await splash.count()) await splash.click();
  await page.locator(".drl-splash").waitFor({ state: "detached", timeout: 10_000 }).catch(() => undefined);
  await page.locator(app === "buyer" ? ".buyer-page" : ".mini-shell").waitFor({ state: "visible", timeout: 10_000 });
}

async function assertKhmerFont(page, context) {
  const sample = "ខ្មែរ សូមស្វាគមន៍";
  const result = await page.evaluate(async (text) => {
    const font = '16px "Noto Sans Khmer"';
    await document.fonts.load(font, text);
    await document.fonts.load(font, "ក្រ");
    await document.fonts.ready;
    const loaded = document.fonts.check(font, text) && document.fonts.check(font, "ក្រ");
    const measure = (value) => {
      const probe = document.createElement("span");
      probe.lang = "km";
      probe.textContent = value;
      probe.style.cssText = "position:absolute;left:-9999px;white-space:nowrap;font:16px \"Noto Sans Khmer\", sans-serif";
      document.body.append(probe);
      const width = probe.getBoundingClientRect().width;
      const fontFamily = getComputedStyle(probe).fontFamily;
      probe.remove();
      return { width, fontFamily };
    };
    const sampleMeasurement = measure(text);
    const clusterWidth = measure("ក្រ").width;
    const partsWidth = measure("ក").width + measure("្").width + measure("រ").width;
    return { loaded, width: sampleMeasurement.width, clusterWidth, partsWidth, fontFamily: sampleMeasurement.fontFamily };
  }, sample);
  const shaped = result.clusterWidth < result.partsWidth * 0.95;
  if (!result.loaded || result.width <= 0 || !result.fontFamily.includes("Noto Sans Khmer") || !shaped) {
    throw new Error(`Khmer font check failed for ${context}: loaded=${result.loaded}, width=${result.width}, clusterWidth=${result.clusterWidth}, partsWidth=${result.partsWidth}, fontFamily=${result.fontFamily}`);
  }
  return { ...result, shaped };
}

const inspectPng = async (path) => {
  const bytes = await readFile(path);
  if (bytes.toString("ascii", 1, 4) !== "PNG") throw new Error(`not a PNG: ${path}`);
  return {
    width: bytes.readUInt32BE(16),
    height: bytes.readUInt32BE(20),
    sha256: createHash("sha256").update(bytes).digest("hex")
  };
};

await mkdir(output, { recursive: true });
const servers = [start("buyer", 5173), start("merchant", 5174)];
try {
  await Promise.all(servers.map(({ port }) => waitFor(port)));
  const browser = await chromium.launch();
  const manifest = [];
  const hashesByScreen = new Map();
  try {
    for (const app of ["buyer", "merchant"]) {
      const port = app === "buyer" ? 5173 : 5174;
      for (const item of matrix) {
        const page = await browser.newPage({ viewport: { width: item.width, height: 900 }, colorScheme: item.theme });
        await page.addInitScript((language) => localStorage.setItem("drlife_lang", language), item.language);
        await installMockApi(page, item.language, app);
        const url = `http://127.0.0.1:${port}/${launchParams(item.theme, item.language)}`;
        await page.goto(url, { waitUntil: "networkidle" });
        await waitForBusinessPage(page, app);
        const khmerFont = item.language === "km"
          ? await assertKhmerFont(page, `${app}-${item.width}-${item.theme}`)
          : null;
        const bodyBackground = await page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor);
        const expectedBackground = item.theme === "dark" ? "rgb(17, 18, 16)" : "rgb(246, 242, 233)";
        if (bodyBackground !== expectedBackground) {
          throw new Error(`Theme background mismatch for ${app}-${item.theme}-${item.language}: ${bodyBackground}`);
        }
        const targets = app === "buyer" ? [
          { name: "home" },
          { name: "profile", open: async () => {
            await page.locator(".buyer-profile-action").click();
            await page.locator(".profile-page").waitFor({ state: "visible", timeout: 5_000 });
          } },
          { name: "wallet", open: async () => {
            await page.locator(".buyer-profile-action").click();
            await page.locator(".profile-page").waitFor({ state: "visible", timeout: 5_000 });
            await page.locator(".profile-card-heading button").first().click();
            await page.locator(".wallet-page").waitFor({ state: "visible", timeout: 5_000 });
          }, profile: true },
          { name: "orders-list", open: async () => {
            await page.locator(".buyer-orders-action").click();
            await page.locator(".buyer-order-list").waitFor({ state: "visible", timeout: 5_000 });
          } },
          { name: "orders-detail", open: async () => {
            await page.locator(".buyer-orders-action").click();
            await page.locator(".buyer-order-list").waitFor({ state: "visible", timeout: 5_000 });
            await page.locator(".buyer-order-card").first().click();
            await page.locator(".buyer-order-detail__heading").first().waitFor({ state: "visible", timeout: 5_000 });
          } },
          { name: "notifications", open: async () => {
            await page.locator(".buyer-notifications-bell").click();
            await page.locator(".buyer-page").waitFor({ state: "visible", timeout: 5_000 });
            await page.locator(".drl-notification-list .drl-list-item").first().waitFor({ state: "visible", timeout: 10_000 });
          } }
        ] : [
          { name: "home" },
          { name: "merchant-orders-list", open: async () => {
            await page.locator(".merchant-orders-action").click();
            await page.locator(".merchant-orders").waitFor({ state: "visible", timeout: 5_000 });
          } },
          { name: "merchant-orders-detail", open: async () => {
            await page.locator(".merchant-orders-action").click();
            await page.locator(".merchant-orders").waitFor({ state: "visible", timeout: 5_000 });
            await page.locator(".merchant-order-item").first().click();
            await page.locator(".merchant-order-detail__heading").waitFor({ state: "visible", timeout: 5_000 });
          } },
          { name: "merchant-profile", open: async () => {
            await page.getByRole("button", { name: /profile|workspace|我的|商家工作台|កន្លែងធ្វើការអ្នកលក់|โปรไฟล์/i }).last().click();
            await page.locator(".merchant-v4").waitFor({ state: "visible", timeout: 5_000 });
          } },
          { name: "merchant-demands", open: async () => {
            await page.getByRole("button", { name: /profile|workspace|我的|商家工作台|កន្លែងធ្វើការអ្នកលក់|โปรไฟล์/i }).last().click();
            await page.locator(".merchant-v4").waitFor({ state: "visible", timeout: 5_000 });
            await page.locator(".merchant-v4__tabs button").nth(1).click();
            await page.locator(".merchant-v4__section").waitFor({ state: "visible", timeout: 5_000 });
          }, profile: true },
          { name: "merchant-warehouse", open: async () => {
            await page.getByRole("button", { name: /profile|workspace|我的|商家工作台|កន្លែងធ្វើការអ្នកលក់|โปรไฟล์/i }).last().click();
            await page.locator(".merchant-v4").waitFor({ state: "visible", timeout: 5_000 });
            await page.locator(".merchant-v4__tabs button").nth(2).click();
            await page.locator(".merchant-v4__warehouse").waitFor({ state: "visible", timeout: 5_000 });
          }, profile: true },
          { name: "merchant-notifications", open: async () => {
            await page.locator(".merchant-v7-action").click();
            await page.locator(".merchant-v7 .drl-notification-bell").click();
            await page.locator(".merchant-v7").waitFor({ state: "visible", timeout: 5_000 });
            await page.locator(".merchant-v7 .drl-notification-list .drl-list-item").first().waitFor({ state: "visible", timeout: 10_000 });
          } }
        ];
        for (const target of targets) {
          await page.goto(url, { waitUntil: "networkidle" });
          await waitForBusinessPage(page, app);
          if (target.open) await target.open();
          const title = await page.title();
          if (/login|error|invalid/i.test(title)) throw new Error(`unexpected Mini App title: ${title}`);
          const headingSelector = app === "buyer"
            ? (target.name === "home" ? ".buyer-section__title" : target.name === "profile" ? ".profile-hero h1" : target.name === "wallet" ? ".wallet-page h1" : target.name === "notifications" ? ".buyer-section__title" : ".buyer-order-list, .buyer-order-detail__heading")
            : target.name === "home" ? "h1" : target.name === "merchant-notifications" ? ".merchant-v7 h1" : target.name.startsWith("merchant-orders-") ? ".merchant-orders, .merchant-order-detail__heading" : ".merchant-v4 h1, .merchant-v4__section h2";
          const pageHeading = await page.locator(headingSelector).first().textContent();
          if (!pageHeading?.trim()) throw new Error(`missing business page heading for ${app}-${target.name}-${item.language}`);
          const name = `${app}-${target.name}-${item.width}-${item.theme}-${item.language}.png`;
          const screenshotPath = join(output, name);
          await page.screenshot({ path: screenshotPath, fullPage: true });
          const image = await inspectPng(screenshotPath);
          if (item.width === 390 && image.width !== 390) {
            throw new Error(`390px evidence has wrong width for ${name}: ${image.width}`);
          }
          const screenKey = `${app}-${target.name}-${item.width}-${item.theme}`;
          const screenHashes = hashesByScreen.get(screenKey) ?? new Map();
          screenHashes.set(item.language, image.sha256);
          hashesByScreen.set(screenKey, screenHashes);
          manifest.push(`${name}\t${title || "untitled"}\t${pageHeading.trim()}\twidth=${image.width}\theight=${image.height}\tsha256=${image.sha256}\tkhmerFont=${khmerFont ? `loaded:${khmerFont.loaded},shaped:${khmerFont.shaped},width:${khmerFont.width},clusterWidth:${khmerFont.clusterWidth},partsWidth:${khmerFont.partsWidth},fontFamily:${khmerFont.fontFamily}` : "n/a"}`);
        }
        await page.close();
      }
    }
  } finally { await browser.close(); }
  for (const [screenKey, hashes] of hashesByScreen) {
    if (hashes.size !== 3 || new Set(hashes.values()).size !== 3) {
      throw new Error(`localized screenshot hashes must differ for ${screenKey}`);
    }
  }
  const head = process.env.EVIDENCE_HEAD;
  await writeFile(join(output, "manifest.txt"), `${head ? `head=${head}\n` : ""}${manifest.join("\n")}\n`);
  console.log(`Captured ${manifest.length} Mini App screenshots in ${output}`);
} finally { for (const { child } of servers) child.kill("SIGTERM"); }
