import assert from "node:assert/strict";
import { createReadStream, existsSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { extname, join, normalize } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

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
  try {
    for (const [locale, localizedMarker] of [["", "生活"], ["en/", "INFINITE"], ["km/", "ជីវិត"]]) {
      const response = await globalThis.fetch(`http://127.0.0.1:${port}/${locale}`);
      assert.equal(response.status, 200);
      const html = await response.text();
      assert.match(html, /<h1[\s\S]*?<\/h1>/);
      assert.equal((html.match(/<article class="partner/g) ?? []).length, 5);
      assert.ok((html.match(/href="[^"]*\/contact\/?"/g) ?? []).length >= 4);
      assert.match(html, new RegExp(localizedMarker));
      assert.match(html, /class="menu-toggle"[\s\S]*?aria-expanded="false"/);
      assert.match(html, /id="site-nav"/);
      assert.match(html, /src="\/scripts\/homepage-interaction\.js"/);
    }
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});
