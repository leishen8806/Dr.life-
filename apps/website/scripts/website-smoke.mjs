import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const baseUrl = process.env.WEBSITE_PUBLIC_URL;
if (!baseUrl) throw new Error("WEBSITE_PUBLIC_URL is required");

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  const response = await page.goto(new URL("/", baseUrl), { waitUntil: "networkidle" });
  if (!response || response.status() !== 200) throw new Error(`homepage status=${response?.status() ?? "no response"}`);
  if (!(await page.locator("h1").first().isVisible())) throw new Error("homepage h1 is not visible");
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
  if (overflow) throw new Error("homepage has horizontal overflow at 390px");
  for (const path of ["/privacy/", "/terms/", "/404/"]) {
    const check = await page.request.get(new URL(path, baseUrl));
    if (check.status() >= 500) throw new Error(`${path} status=${check.status()}`);
  }
  await mkdir("artifacts/website-smoke", { recursive: true });
  await page.screenshot({ path: "artifacts/website-smoke/home-390.png", fullPage: true });
  console.log(`Website smoke passed for ${baseUrl}`);
} finally {
  await browser.close();
}
