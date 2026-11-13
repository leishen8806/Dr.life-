import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const output = fileURLToPath(new URL("../screenshots-miniapp/", import.meta.url));
const matrix = [360, 430].flatMap((width) =>
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
      await route.fulfill(json({ items: [], next_cursor: null }));
      return;
    }
    if (path === "/v1/merchant/products" && request.method() === "GET") {
      await route.fulfill(json({ items: [] }));
      return;
    }
    if (path === "/v1/merchant/suborders" && request.method() === "GET") {
      await route.fulfill(json({ items: [], next_cursor: null }));
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

await mkdir(output, { recursive: true });
const servers = [start("buyer", 5173), start("merchant", 5174)];
try {
  await Promise.all(servers.map(({ port }) => waitFor(port)));
  const browser = await chromium.launch();
  const manifest = [];
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
        const bodyBackground = await page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor);
        const expectedBackground = item.theme === "dark" ? "rgb(17, 18, 16)" : "rgb(246, 242, 233)";
        if (bodyBackground !== expectedBackground) {
          throw new Error(`Theme background mismatch for ${app}-${item.theme}-${item.language}: ${bodyBackground}`);
        }
        const targets = app === "buyer" ? [
          { name: "home" },
          { name: "orders", open: async () => {
            await page.locator(".buyer-orders-action").click();
            await page.locator(".buyer-order-list, .drl-state").first().waitFor({ state: "visible", timeout: 5_000 });
          } }
        ] : [{ name: "home" }];
        for (const target of targets) {
          if (target.open) await target.open();
          const title = await page.title();
          if (/login|error|invalid/i.test(title)) throw new Error(`unexpected Mini App title: ${title}`);
          const headingSelector = app === "buyer"
            ? (target.name === "orders" ? ".drl-state p, .buyer-order-list" : ".buyer-section__title")
            : "h1";
          const pageHeading = await page.locator(headingSelector).first().textContent();
          if (!pageHeading?.trim()) throw new Error(`missing business page heading for ${app}-${target.name}-${item.language}`);
          const name = `${app}-${target.name}-${item.width}-${item.theme}-${item.language}.png`;
          await page.screenshot({ path: join(output, name), fullPage: true });
          manifest.push(`${name}\t${title || "untitled"}\t${pageHeading.trim()}`);
        }
        await page.close();
      }
    }
  } finally { await browser.close(); }
  await writeFile(join(output, "manifest.txt"), `${manifest.join("\n")}\n`);
  console.log(`Captured ${manifest.length} Mini App screenshots in ${output}`);
} finally { for (const { child } of servers) child.kill("SIGTERM"); }
