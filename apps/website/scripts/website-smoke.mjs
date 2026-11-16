import { mkdir, writeFile } from "node:fs/promises";
import { chromium } from "playwright";

const baseUrl = process.env.WEBSITE_PUBLIC_URL;
if (!baseUrl) throw new Error("WEBSITE_PUBLIC_URL is required");

const browser = await chromium.launch();
try {
  const menuScreenshots = [];
  for (const [path, locale] of [["/", "zh"], ["/en/", "en"], ["/km/", "km"]]) {
    const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
    const response = await page.goto(new URL(path, baseUrl).toString(), { waitUntil: "networkidle" });
    if (!response || response.status() !== 200) throw new Error(`${path} status=${response?.status() ?? "no response"}`);
    if (!(await page.locator("h1").first().isVisible())) throw new Error(`${path} homepage h1 is not visible`);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
    if (overflow) throw new Error(`${path} homepage has horizontal overflow at 390px`);

    const interactionScript = page.locator('script[src="/scripts/homepage-interaction.js"]');
    if (await interactionScript.count() !== 1) throw new Error(`${path} interaction script is missing`);
    const menu = page.locator(".menu-toggle");
    const nav = page.locator("#site-nav");
    await menu.click();
    if (await menu.getAttribute("aria-expanded") !== "true" || !(await nav.isVisible())) throw new Error(`${path} menu did not open`);
    if (!(await nav.locator('a[href="/en/"]').isVisible()) || !(await nav.locator('a[href="/km/"]').isVisible())) throw new Error(`${path} language links are not visible in menu`);
    const screenshotPath = `artifacts/website-smoke/home-${locale}-390-menu.png`;
    await page.screenshot({ path: screenshotPath, fullPage: true });
    menuScreenshots.push({ locale, path: screenshotPath, viewport: { width: 390, height: 844 }, state: "menu-expanded" });
    await page.keyboard.press("Escape");
    if (await menu.getAttribute("aria-expanded") !== "false" || await nav.isVisible()) throw new Error(`${path} Escape did not close menu`);
    await menu.click();
    await nav.locator('a[href="#market"]').click();
    if (!page.url().endsWith("#market") || !(await page.locator("#market").isVisible())) throw new Error(`${path} second section navigation did not change content`);
    await page.close();
  }
  await writeFile("artifacts/website-smoke/menu-manifest.json", JSON.stringify({ headSha: process.env.GITHUB_SHA ?? "local", baseUrl, screenshots: menuScreenshots }, null, 2));

  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  await page.goto(new URL("/", baseUrl).toString(), { waitUntil: "networkidle" });
  for (const path of ["/privacy/", "/terms/", "/404/"]) {
    const check = await page.request.get(new URL(path, baseUrl).toString());
    if (check.status() >= 500) throw new Error(`${path} status=${check.status()}`);
  }
  await mkdir("artifacts/website-smoke", { recursive: true });
  await page.screenshot({ path: "artifacts/website-smoke/home-390.png", fullPage: true });
  console.log(`Website smoke passed for ${baseUrl}`);
} finally {
  await browser.close();
}
