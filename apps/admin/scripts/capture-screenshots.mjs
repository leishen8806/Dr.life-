import { mkdir, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { chromium } from "../../website/node_modules/playwright/index.mjs";

const pages = [
  { name: "login", path: "/login" },
  { name: "administrators", path: "/administrators" },
  { name: "audit", path: "/audit" }
];
const widths = [1440, 390];
const languages = ["zh", "en", "km"];

function waitForServer(url) {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error("admin dev server did not start")), 30000);
    const check = async () => {
      try {
        const response = await fetch(url);
        if (response.ok) { clearTimeout(timeout); resolve(); return; }
      } catch {}
      setTimeout(check, 250);
    };
    check();
  });
}

const server = spawn("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", "5176", "--strictPort"], {
  cwd: new URL("../", import.meta.url).pathname,
  stdio: "ignore",
  env: { ...process.env, VITE_ADMIN_SCREENSHOT: "true" }
});
await waitForServer("http://127.0.0.1:5176/");
const browser = await chromium.launch();
const manifest = [];
try {
  for (const width of widths) for (const language of languages) for (const pageDefinition of pages) {
    const context = await browser.newContext({ viewport: { width, height: 900 }, locale: language === "km" ? "km-KH" : "zh-CN" });
    await context.addInitScript((selectedLanguage) => localStorage.setItem("drlife_lang", selectedLanguage), language);
    const page = await context.newPage();
    await page.goto("http://127.0.0.1:5176" + pageDefinition.path, { waitUntil: "networkidle" });
    await page.waitForTimeout(250);
    const relative = width + "/" + language + "/" + pageDefinition.name + ".png";
    await mkdir(new URL("../screenshots/" + width + "/" + language + "/", import.meta.url), { recursive: true });
    await page.screenshot({ path: new URL("../screenshots/" + relative, import.meta.url).pathname, fullPage: true });
    manifest.push(relative);
    await context.close();
  }
} finally {
  await browser.close();
  server.kill("SIGTERM");
}
const evidenceHead = process.env.EVIDENCE_HEAD;
await writeFile(new URL("../screenshots/manifest.txt", import.meta.url), `${evidenceHead ? `head=${evidenceHead}\n` : ""}${manifest.join("\n")}\n`);
console.log("Captured " + manifest.length + " admin screenshots");
