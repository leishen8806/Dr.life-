import assert from "node:assert/strict";
import { createReadStream, existsSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { extname, join, normalize } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const dist = fileURLToPath(new URL("../dist/", import.meta.url));
const mimeTypes = { ".css": "text/css", ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".json": "application/json", ".png": "image/png", ".svg": "image/svg+xml", ".webp": "image/webp", ".avif": "image/avif" };

function startStaticServer() {
  const server = createServer((request, response) => {
    const requestPath = decodeURIComponent(new URL(request.url ?? "/", "http://localhost").pathname);
    const relative = requestPath === "/" ? "index.html" : requestPath.replace(/^\//, "");
    const candidate = normalize(join(dist, relative));
    const file = existsSync(candidate) && statSync(candidate).isFile()
      ? candidate
      : join(dist, relative.endsWith("/") ? `${relative}index.html` : `${relative}/index.html`);
    if (!file.startsWith(dist) || !existsSync(file) || !statSync(file).isFile()) {
      response.writeHead(404).end("not found");
      return;
    }
    response.setHeader("Content-Type", mimeTypes[extname(file)] ?? "application/octet-stream");
    createReadStream(file).pipe(response);
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

test("homepage V13 behavior stays localized and usable at mobile width", async () => {
  const server = await startStaticServer();
  const { port } = server.address();
  const browser = await chromium.launch({ headless: true });
  try {
    for (const [locale, localizedMarker] of [["", "生活"], ["en/", "INFINITE"], ["km/", "ជីវិត"]]) {
      const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
      await page.goto(`http://127.0.0.1:${port}/${locale}`, { waitUntil: "networkidle" });
      await page.locator("h1").waitFor();
      assert.equal(await page.locator("article.partner").count(), 5);
      assert.equal(await page.locator('a[href$="/contact"]').count(), 4);
      const bodyText = await page.locator("body").innerText();
      assert.ok(bodyText.trim().length > 0);
      assert.match(bodyText, new RegExp(localizedMarker));
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
      await page.getByRole("button", { name: /菜单|Menu|ម៉ឺនុយ/i }).click();
      assert.equal(await page.locator("#site-nav").getAttribute("data-open"), "true");
      await page.close();
    }
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }
});
