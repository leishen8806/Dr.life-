import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const output = join(root, "evidence-pr235");
const matrix = ["light", "dark"].flatMap((theme) => ["zh", "km"].map((language) => ({ theme, language })));
const api = (data) => ({ status: 200, contentType: "application/json", body: JSON.stringify({ data }) });
const launch = (theme, language) => `?tgWebAppPlatform=tdesktop&tgWebAppVersion=7.0&tgWebAppData=${encodeURIComponent(`user=${encodeURIComponent(JSON.stringify({ id: 1, language_code: language }))}`)}&tgWebAppThemeParams=${encodeURIComponent(JSON.stringify({ bg_color: theme === "dark" ? "#111315" : "#f4f6f8", text_color: theme === "dark" ? "#f4f6f8" : "#111315", color_scheme: theme }))}`;
const suborder = { public_id: "01ORDEREVIDENCE", parent_public_id: "01PARENTEVIDENCE", status: "paid", items: [{ sku_public_id: "SKU-001", quantity: 2, unit_price_e4: 125000, line_total_e4: 250000 }], shipping_address_public_id: "01ADDREVIDENCE", shipping_address: { recipient_name: "Evidence Buyer", address_text: "Phnom Penh", phone: "****1234" }, goods_amount_e4: 250000, shipping_amount_e4: 0, payable_amount_e4: 250000, shipping_quote_expires_at: "2030-01-01T00:00:00Z", created_at: "2030-01-01T00:00:00Z", updated_at: "2030-01-01T00:00:00Z" };
const start = (app, port) => spawn(join(root, "node_modules/.bin/vite"), ["--host", "127.0.0.1", "--port", String(port), "--strictPort"], { cwd: join(root, "apps", app), stdio: "inherit" });
const wait = async (port) => { for (let i = 0; i < 120; i++) { try { if ((await fetch(`http://127.0.0.1:${port}`)).ok) return; } catch {} await new Promise((r) => setTimeout(r, 250)); } throw new Error(`server ${port} did not start`); };
const contrast = (foreground, background) => {
  const parse = (value) => value.match(/\d+(?:\.\d+)?/g)?.slice(0, 3).map(Number) ?? [];
  const luminance = (value) => parse(value).map((channel) => channel / 255).map((channel) => channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4).reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index], 0);
  const light = luminance(foreground);
  const dark = luminance(background);
  return (Math.max(light, dark) + 0.05) / (Math.min(light, dark) + 0.05);
};
const servers = [start("buyer", 5173), start("merchant", 5174)];
await Promise.all([wait(5173), wait(5174)]);
await mkdir(output, { recursive: true });
const browser = await chromium.launch();
const manifest = [];
try {
  for (const app of ["buyer", "merchant"]) for (const item of matrix) {
    const page = await browser.newPage({ viewport: { width: 430, height: 900 }, colorScheme: item.theme });
    await page.addInitScript((language) => localStorage.setItem("drlife_lang", language), item.language);
    await page.route("**/v1/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/v1/public/auth/telegram") return route.fulfill(api({ access_token: "evidence", refresh_token: "evidence", account: { public_id: "01EVIDENCE", role: app, lang: item.language, status: "active", needs_language: false } }));
      if (path === "/v1/public/auth/refresh") return route.fulfill(api({ access_token: "evidence", refresh_token: "evidence" }));
      if (path.endsWith("/me/consents")) return route.fulfill(api({ items: [] }));
      if (path === "/v1/public/categories" || path === "/v1/public/products" || path === "/v1/merchant/products") return route.fulfill(api({ items: [], total: 0, page: 1, page_size: 100 }));
      if (path === "/v1/merchant/suborders") return route.fulfill(api({ items: [suborder], next_cursor: null }));
      if (path === "/v1/merchant/suborders/01orderevidence") return route.fulfill(api(suborder));
      if (path === "/v1/buyer/orders") return route.fulfill(api({ items: [], next_cursor: null }));
      return route.continue();
    });
    await page.goto(`http://127.0.0.1:${app === "buyer" ? 5173 : 5174}/${launch(item.theme, item.language)}`, { waitUntil: "networkidle" });
    const skip = page.locator(".drl-splash__skip");
    if (await skip.count()) await skip.click();
    await page.locator(".drl-splash").waitFor({ state: "detached", timeout: 10000 }).catch(() => undefined);
    const background = await page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor);
    const expectedBackground = item.theme === "dark" ? "rgb(17, 18, 16)" : "rgb(246, 242, 233)";
    if (background !== expectedBackground) throw new Error(`Theme background mismatch for ${app}-${item.theme}-${item.language}: ${background}`);
    if (item.theme === "dark") {
      const contrastChecks = await page.locator(".drl-language-switcher .drl-button:not([aria-pressed='true']), .drl-status-tag--neutral").evaluateAll((nodes) => nodes.map((node) => {
        const style = getComputedStyle(node);
        return { text: style.color, background: style.backgroundColor, label: node.textContent?.trim() ?? node.className };
      }));
      if (!contrastChecks.length) throw new Error(`Dark contrast targets missing for ${app}-${item.language}`);
      for (const item of contrastChecks) {
        const ratio = contrast(item.text, item.background);
        if (ratio < 4.5) throw new Error(`Dark contrast ${ratio.toFixed(2)}:1 for ${app}-${item.language} ${item.label}`);
      }
    }
    if (app === "buyer") {
      await page.locator(".buyer-page").waitFor({ state: "visible" });
      await page.locator(".buyer-section__title").first().waitFor({ state: "visible" });
      const name = `buyer-home-430-${item.theme}-${item.language}.png`;
      await page.screenshot({ path: join(output, name), fullPage: true }); manifest.push(name);
    } else {
      await page.locator(".mini-shell").waitFor({ state: "visible" });
      await page.locator("button").nth(3).click();
      await page.locator(".merchant-order-item").first().waitFor({ state: "visible" });
      const list = `merchant-orders-430-${item.theme}-${item.language}.png`;
      await page.screenshot({ path: join(output, list), fullPage: true }); manifest.push(list);
      await page.locator(".merchant-order-item").first().click();
      await page.locator(".merchant-order-total").waitFor({ state: "visible" });
      const detail = `merchant-order-detail-430-${item.theme}-${item.language}.png`;
      await page.screenshot({ path: join(output, detail), fullPage: true }); manifest.push(detail);
    }
    await page.close();
  }
} finally { await browser.close(); for (const server of servers) server.kill("SIGTERM"); }
await writeFile(join(output, "manifest.txt"), `${manifest.join("\n")}\n`);
console.log(`Captured ${manifest.length} PR235 screenshots`);
