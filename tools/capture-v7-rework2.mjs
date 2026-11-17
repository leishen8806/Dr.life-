import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "../apps/website/node_modules/playwright/index.mjs";

const root = resolve(new URL("..", import.meta.url).pathname);
const out = resolve(new URL("../evidence/pr-279/screenshots/", import.meta.url).pathname);
const matrix = [360, 430].flatMap((width) => ["light", "dark"].flatMap((theme) => ["zh", "en", "km"].map((language) => ({ width, theme, language }))));
const launch = (theme, language) => `?tgWebAppPlatform=tdesktop&tgWebAppVersion=7.0&tgWebAppData=${encodeURIComponent(`user=${encodeURIComponent(JSON.stringify({ id: 1, language_code: language }))}`)}&tgWebAppThemeParams=${encodeURIComponent(JSON.stringify({ bg_color: theme === "dark" ? "#111315" : "#f4f6f8", text_color: theme === "dark" ? "#f4f6f8" : "#111315", color_scheme: theme }))}`;
const mock = (data) => ({ status: 200, contentType: "application/json", body: JSON.stringify({ data }) });
const children = [];
const start = (app, port) => children.push(spawn(join(root, "node_modules/.bin/vite"), ["--host", "127.0.0.1", "--port", String(port), "--strictPort"], { cwd: join(root, "apps", app), stdio: "inherit" }));
const wait = async (port) => { for (let i = 0; i < 120; i += 1) { try { if ((await fetch(`http://127.0.0.1:${port}`)).ok) return; } catch {} await new Promise((r) => setTimeout(r, 250)); } throw new Error(`vite ${port} did not start`); };
const ready = async (page, selector) => { await page.locator(".drl-splash__skip").click().catch(() => undefined); await page.locator(".drl-splash").waitFor({ state: "detached", timeout: 10000 }).catch(() => undefined); await page.locator(selector).waitFor({ state: "visible", timeout: 10000 }); };
const setup = async (page, app, language) => { await page.route("**/v1/**", async (route) => { const path = new URL(route.request().url()).pathname; if (path.includes("/auth/")) return route.fulfill(mock({ access_token: "evidence", refresh_token: "evidence", account: { public_id: "evidence", role: app, lang: language, status: "active", needs_language: false } })); if (path.endsWith("/me/consents")) return route.fulfill(mock({ items: [] })); if (["categories", "products", "orders", "suborders"].some((part) => path.includes(part))) return route.fulfill(mock({ items: [], next_cursor: null, total: 0, page: 1, page_size: 100 })); return route.continue(); }); };
const manifest = [];
const shot = async (page, app, name, item) => { const file = `${app}-${name}-${item.width}-${item.theme}-${item.language}.png`; await page.screenshot({ path: join(out, file), fullPage: true }); if (await page.evaluate(() => document.documentElement.scrollWidth) > item.width) throw new Error(`${file} overflow`); manifest.push(file); };
const openHome = async (page, app, item) => { const port = app === "buyer" ? 5173 : 5174; await page.goto(`http://127.0.0.1:${port}/${launch(item.theme, item.language)}`, { waitUntil: "domcontentloaded" }); await ready(page, app === "buyer" ? ".buyer-page" : ".mini-shell"); };
await mkdir(out, { recursive: true }); start("buyer", 5173); start("merchant", 5174); await Promise.all([wait(5173), wait(5174)]); const browser = await chromium.launch();
try {
  for (const item of matrix) for (const app of ["buyer", "merchant"]) {
    const page = await browser.newPage({ viewport: { width: item.width, height: 900 }, colorScheme: item.theme });
    page.setDefaultTimeout(3000);
    await page.addInitScript((language) => localStorage.setItem("drlife_lang", language), item.language);
    await setup(page, app, item.language);
    await openHome(page, app, item);
    await shot(page, app, "home", item);
    if (app === "buyer") {
      await page.locator(".buyer-decor-action").click(); await ready(page, ".v7-tabs"); await shot(page, app, "decor", item);
      await page.getByRole("button", { name: /view company|查看企业|មើលសហគ្រាស/i }).first().click(); await shot(page, app, "company", item);
      await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.getByRole("button", { name: /cases|案例|ករណី/i }).click(); await page.getByRole("button", { name: /warm apartment|温馨公寓|ផ្ទះល្វែង/i }).click(); await shot(page, app, "case-detail", item);
      await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.locator(".buyer-notifications-action").click(); await page.locator(".drl-notification-list .drl-list-item").first().click(); await shot(page, app, "aftercare", item);
      await page.locator(".v7-choice-grid button").first().click(); await page.locator(".buyer-page").getByRole("button").filter({ hasText: /confirm|确认|បញ្ជាក់|✓/i }).click(); await shot(page, app, "aftercare-confirmed", item);
      await page.locator(".buyer-page").getByRole("button").filter({ hasText: /report|问题|បញ្ហា/i }).click(); const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"); await page.locator('input[type="file"]').setInputFiles({ name: "evidence.png", mimeType: "image/png", buffer: png }); await shot(page, app, "feedback-preview", item);
      await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.locator(".buyer-notifications-action").click(); await shot(page, app, "notifications", item);
    } else {
      await page.locator(".merchant-v7-action").click(); await ready(page, ".merchant-v7"); await shot(page, app, "dashboard", item);
      await page.locator(".merchant-v7 .drl-notification-bell").click(); await shot(page, app, "notifications-settings", item); await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click();
      await page.locator(".merchant-v7-profile-action").click(); await shot(page, app, "profile", item); await page.getByRole("button", { name: /edit|编辑|កែប្រែ/i }).click(); await shot(page, app, "profile-edit", item);
      await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.locator(".merchant-v7-profile-action").click(); await page.getByRole("button", { name: /business onboarding|企业入驻|ចុះឈ្មោះសហគ្រ/i }).click(); await shot(page, app, "onboarding", item); const license = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"); await page.locator('input[type="file"]').setInputFiles({ name: "license.png", mimeType: "image/png", buffer: license }); await shot(page, app, "onboarding-license-preview", item);
      await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.getByRole("button", { name: /back|返回|ត្រឡប់/i }).first().click(); await page.locator(".merchant-v7__entries").getByRole("button", { name: /import|导入|នាំចូល/i }).click(); await shot(page, app, "import-source-preview", item); await page.locator('input[type="checkbox"]').check(); await page.getByRole("button", { name: /import|导入|នាំចូល/i }).last().click(); await shot(page, app, "import-pending", item);
    }
    await page.close();
  }
} finally { await browser.close(); for (const child of children) child.kill("SIGTERM"); }
await writeFile(join(out, "manifest.txt"), `head=${process.env.EVIDENCE_HEAD ?? "local"}\n${manifest.join("\n")}\n`);
console.log(`Captured ${manifest.length} V7 screenshots`);
