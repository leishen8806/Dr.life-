import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";

const root = fileURLToPath(new URL(".", import.meta.url));
const commit = execSync("git rev-parse --short HEAD", { cwd: root })
  .toString()
  .trim();
const stamp = new Date().toISOString().replace(/[:.]/g, "-");
const buildId = `${commit}-${stamp}`;
const proxy = {
  "/v1": "http://127.0.0.1:8787",
  "/__mock": "http://127.0.0.1:8787"
};

export default defineConfig({
  root,
  define: {
    __BUILD_ID__: JSON.stringify(buildId)
  },
  server: {
    port: 5177,
    strictPort: true,
    proxy
  },
  preview: {
    port: 5180,
    strictPort: true,
    proxy
  },
  build: {
    outDir: "dist",
    emptyOutDir: true
  },
  plugins: [
    {
      name: "stamp-service-worker",
      apply: "build",
      closeBundle() {
        const worker = path.join(root, "dist", "sw.js");
        if (!existsSync(worker)) {
          throw new Error("dist/sw.js was not emitted");
        }
        const assetDir = path.join(root, "dist", "assets");
        const assets = existsSync(assetDir)
          ? readdirSync(assetDir)
              .filter((file) => !file.endsWith(".map"))
              .map((file) => `/assets/${file}`)
          : [];
        const precacheUrls = ["/index.html", ...assets];
        writeFileSync(
          worker,
          readFileSync(worker, "utf8")
            .replaceAll("__BUILD_ID__", buildId)
            .replaceAll("__PRECACHE_URLS__", JSON.stringify(precacheUrls))
        );
      }
    }
  ]
});
