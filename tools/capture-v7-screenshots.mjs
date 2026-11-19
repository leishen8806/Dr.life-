import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "../apps/website/node_modules/playwright/index.mjs";

const root = resolve(new URL("..", import.meta.url).pathname);
const output = resolve(
  new URL("../evidence/pr-292/screenshots/", import.meta.url).pathname
);
const matrix = [360, 430].flatMap((width) =>
  ["light", "dark"].flatMap((theme) =>
    ["zh", "en", "km"].map((language) => ({ width, theme, language }))
  )
);
const servers = [];
const launch = (theme, language) =>
  `?tgWebAppPlatform=tdesktop&tgWebAppVersion=7.0&tgWebAppData=${encodeURIComponent(`user=${encodeURIComponent(JSON.stringify({ id: 1, language_code: language }))}`)}&tgWebAppThemeParams=${encodeURIComponent(JSON.stringify({ bg_color: theme === "dark" ? "#111315" : "#f4f6f8", text_color: theme === "dark" ? "#f4f6f8" : "#111315", color_scheme: theme }))}`;
const json = (data) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ data })
});
function start(app, port) {
  const child = spawn(
    join(root, "node_modules/.bin/vite"),
    ["--host", "127.0.0.1", "--port", String(port), "--strictPort"],
    { cwd: join(root, "apps", app), stdio: "inherit" }
  );
  servers.push(child);
  return port;
}
async function waitFor(port) {
  for (let i = 0; i < 120; i += 1) {
    try {
      if ((await fetch(`http://127.0.0.1:${port}`)).ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`vite ${port} did not start`);
}
async function mock(page, app, language) {
  await page.route("**/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (
      path === "/v1/public/auth/telegram" ||
      path === "/v1/public/auth/refresh"
    )
      return route.fulfill(
        json({
          access_token: "evidence",
          refresh_token: "evidence",
          account: {
            public_id: "evidence",
            role: app,
            lang: language,
            status: "active",
            needs_language: false
          }
        })
      );
    if (path.endsWith("/me/consents"))
      return route.fulfill(json({ items: [] }));
    if (path === "/v1/public/categories")
      return route.fulfill(
        json({ items: [], total: 0, page: 1, page_size: 100 })
      );
    if (path === "/v1/public/products")
      return route.fulfill(
        json({ items: [], total: 0, page: 1, page_size: 20 })
      );
    if (path === "/v1/buyer/orders")
      return route.fulfill(json({ items: [], next_cursor: null }));
    if (path === "/v1/merchant/products")
      return route.fulfill(json({ items: [] }));
    if (path === "/v1/merchant/suborders")
      return route.fulfill(json({ items: [], next_cursor: null }));
    if (
      path === "/v1/buyer/me/notifications" ||
      path === "/v1/merchant/me/notifications"
    )
      return route.fulfill(
        json({
          items: [
            {
              public_id: "evidence-notification",
              event_type: "order.created",
              title: { en: "Evidence notification" },
              body: { en: "Your latest update is ready." },
              read_at: null,
              created_at: "2026-01-01T00:00:00Z"
            }
          ],
          unread_count: 1
        })
      );
    if (
      path === "/v1/buyer/me/notification-settings" ||
      path === "/v1/merchant/me/notification-settings"
    )
      return route.fulfill(json({ items: [] }));
    if (
      path.endsWith("/notifications/read") ||
      path.endsWith("/notifications/read-all") ||
      path.endsWith("/notification-settings")
    )
      return route.fulfill(json({}));
    return route.continue();
  });
}
async function ready(page, selector) {
  const skip = page.locator(".drl-splash__skip");
  if (await skip.count()) await skip.click();
  await page
    .locator(".drl-splash")
    .waitFor({ state: "detached", timeout: 10_000 })
    .catch(() => undefined);
  await page.locator(selector).waitFor({ state: "visible", timeout: 10_000 });
}
async function shot(page, app, name, item, manifest) {
  const filename = `${app}-${name}-${item.width}-${item.theme}-${item.language}.png`;
  await page.screenshot({ path: join(output, filename), fullPage: true });
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  if (width > item.width) throw new Error(`${filename} overflows: ${width}`);
  manifest.push(filename);
}
await mkdir(output, { recursive: true });
const manifest = [];
start("buyer", 5173);
start("merchant", 5174);
await Promise.all([waitFor(5173), waitFor(5174)]);
const browser = await chromium.launch();
try {
  for (const item of matrix) {
    for (const app of ["buyer", "merchant"]) {
      const page = await browser.newPage({
        viewport: { width: item.width, height: 900 },
        colorScheme: item.theme
      });
      await page.addInitScript(
        (language) => localStorage.setItem("drlife_lang", language),
        item.language
      );
      await mock(page, app, item.language);
      await page.goto(
        `http://127.0.0.1:${app === "buyer" ? 5173 : 5174}/${launch(item.theme, item.language)}`,
        { waitUntil: "networkidle" }
      );
      await ready(page, app === "buyer" ? ".buyer-page" : ".mini-shell");
      await shot(page, app, "home", item, manifest);
      if (app === "buyer") {
        await page.getByTestId("buyer-decor").click();
        await ready(page, ".v7-tabs");
        await shot(page, app, "decor", item, manifest);
        await page.getByTestId("buyer-company").first().click();
        await shot(page, app, "company", item, manifest);
        await page.getByTestId("buyer-back").first().click();
        await page.getByTestId("buyer-tab-cases").click();
        await page.getByTestId("buyer-case-case-1").click();
        await shot(page, app, "case-detail", item, manifest);
        await page.goto(
          `http://127.0.0.1:5173/${launch(item.theme, item.language)}`,
          { waitUntil: "networkidle" }
        );
        await ready(page, ".buyer-page");
        await page.goto(
          `http://127.0.0.1:5173/${launch(item.theme, item.language)}&evidence=aftercare`,
          { waitUntil: "networkidle" }
        );
        await ready(page, ".buyer-page");
        await shot(page, app, "aftercare", item, manifest);
        await page.getByTestId("buyer-receipt-photo").click();
        await page.getByTestId("buyer-confirm-receipt").click();
        await shot(page, app, "aftercare-confirmed", item, manifest);
        await page.getByTestId("buyer-report-problem").click();
        const file = Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
          "base64"
        );
        await page
          .locator('input[type="file"]')
          .setInputFiles({
            name: "evidence.png",
            mimeType: "image/png",
            buffer: file
          })
          .catch(() => undefined);
        await shot(page, app, "feedback-preview", item, manifest);
        await page.goto(
          `http://127.0.0.1:5173/${launch(item.theme, item.language)}`,
          { waitUntil: "networkidle" }
        );
        await ready(page, ".buyer-page");
        await page.getByTestId("buyer-notifications").click();
        await shot(page, app, "notifications", item, manifest);
      } else {
        await page.getByTestId("merchant-v7").click();
        await ready(page, ".merchant-v7");
        await shot(page, app, "dashboard", item, manifest);
        await page
          .locator(".merchant-v7")
          .getByTestId("merchant-notifications")
          .click();
        await shot(page, app, "notifications-settings", item, manifest);
        await page.getByTestId("merchant-back").first().click();
        await page.getByTestId("merchant-profile").click();
        await shot(page, app, "profile", item, manifest);
        await page.getByTestId("merchant-profile-edit").click();
        await shot(page, app, "profile-edit", item, manifest);
        await page.getByTestId("merchant-back").first().click();
        await page
          .getByTestId("merchant-profile")
          .click()
          .catch(() => undefined);
        await page.getByTestId("merchant-onboarding").click();
        await shot(page, app, "onboarding", item, manifest);
        await page.getByTestId("merchant-back").click();
        await page
          .getByTestId("merchant-profile")
          .click()
          .catch(() => undefined);
        await page
          .getByTestId("merchant-back")
          .first()
          .click()
          .catch(() => undefined);
        await page
          .locator(".merchant-v7__entries")
          .getByTestId("merchant-import")
          .click();
        await shot(page, app, "import-source-preview", item, manifest);
        await page.locator('input[type="checkbox"]').check();
        await page.getByTestId("merchant-import").last().click();
        await shot(page, app, "import-pending", item, manifest);
      }
      await page.close();
    }
  }
} finally {
  await browser.close();
  for (const child of servers) child.kill("SIGTERM");
}
await writeFile(
  join(output, "manifest.txt"),
  `head=${process.env.EVIDENCE_HEAD ?? "local"}\n${manifest.join("\n")}\n`
);
console.log(`Captured ${manifest.length} V7 screenshots`);
