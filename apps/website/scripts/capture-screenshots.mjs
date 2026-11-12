import { createReadStream, existsSync, statSync } from "node:fs";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join, normalize, posix, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../dist/", import.meta.url));
const output = fileURLToPath(new URL("../screenshots/", import.meta.url));
const widths = [320, 360, 375, 390, 414, 430, 600, 768, 1024, 1440, 1920];
const languages = ["zh", "en", "km"];
const pages = [{ name: "home", path: "/" }, { name: "merchant", path: "/merchant/" }];
const contentTypes = {
  ".css": "text/css", ".html": "text/html; charset=utf-8", ".ico": "image/x-icon",
  ".js": "text/javascript", ".json": "application/json", ".svg": "image/svg+xml",
  ".woff2": "font/woff2", ".webp": "image/webp", ".avif": "image/avif",
  ".png": "image/png", ".jpg": "image/jpeg"
};

function safeFileForUrl(requestUrl) {
  const pathname = new URL(requestUrl, "http://127.0.0.1").pathname;
  const requestedPath = pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
  const candidate = normalize(join(root, requestedPath));
  const relativePath = relative(root, candidate);
  if (relativePath.startsWith("..") || posix.isAbsolute(relativePath)) return null;
  if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  if (existsSync(join(candidate, "index.html"))) return join(candidate, "index.html");
  return null;
}

async function serve() {
  const server = createServer((request, response) => {
    const file = safeFileForUrl(request.url ?? "/");
    if (!file) { response.writeHead(404); response.end("Not found"); return; }
    response.setHeader("Content-Type", contentTypes[extname(file)] ?? "application/octet-stream");
    createReadStream(file).pipe(response);
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Unable to start screenshot server");
  return { server, origin: `http://127.0.0.1:${address.port}` };
}

await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
const { server, origin } = await serve();
const browser = await chromium.launch();
const manifest = [];

try {
  for (const width of widths) {
    for (const language of languages) {
      for (const pageDefinition of pages) {
        const context = await browser.newContext({
          viewport: { width, height: 900 },
          locale: language === "zh" ? "zh-CN" : language === "km" ? "km-KH" : "en-US",
          deviceScaleFactor: 1
        });
        const page = await context.newPage();
        const failedResources = [];
        page.on("response", (resourceResponse) => {
          if (resourceResponse.request().resourceType() !== "document" && resourceResponse.status() >= 400) {
            failedResources.push(`${resourceResponse.status()} ${resourceResponse.url()}`);
          }
        });
        const url = new URL(pageDefinition.path, origin);
        url.searchParams.set("lang", language);
        const response = await page.goto(url.toString(), { waitUntil: "networkidle" });
        if (!response?.ok()) throw new Error(`Screenshot page failed: ${url} (${response?.status() ?? "no response"})`);
        if (failedResources.length > 0) {
          throw new Error(`Screenshot page loaded failed static resources: ${failedResources.join(", ")}`);
        }
        await page.waitForTimeout(150);
        const relative = posix.join(String(width), language, `${pageDefinition.name}.png`);
        await mkdir(join(output, String(width), language), { recursive: true });
        await page.screenshot({ path: join(output, relative), fullPage: true });
        manifest.push(`${relative}\t${url.pathname}?lang=${language}`);
        await context.close();
      }
    }
  }
} finally {
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}

await writeFile(join(output, "manifest.txt"), `${manifest.join("\n")}\n`, "utf8");
const expected = widths.length * languages.length * pages.length;
if (manifest.length !== expected) throw new Error(`Expected ${expected} screenshots, captured ${manifest.length}`);
console.log(`Captured ${manifest.length} screenshots in ${output}`);
