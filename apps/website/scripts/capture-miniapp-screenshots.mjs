import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const output = fileURLToPath(new URL("../screenshots-miniapp/", import.meta.url));
const matrix = [360, 430].flatMap((width) =>
  ["light", "dark"].flatMap((theme) =>
    ["zh", "en", "km"].map((language) => ({ width, theme, language }))
  )
);

const launchParams = (theme, language) => {
  const data = encodeURIComponent(`user=${encodeURIComponent(JSON.stringify({ id: 1, language_code: language }))}`);
  const themeParams = encodeURIComponent(JSON.stringify({
    bg_color: theme === "dark" ? "#111315" : "#f4f6f8",
    text_color: theme === "dark" ? "#f4f6f8" : "#111315"
  }));
  return `?tgWebAppPlatform=tdesktop&tgWebAppVersion=7.0&tgWebAppData=${data}&tgWebAppThemeParams=${themeParams}`;
};

const start = (app, port) => {
  const child = spawn("pnpm", ["dev"], { cwd: join(root, "apps", app), stdio: "inherit" });
  return { child, port };
};

const waitFor = async (port) => {
  for (let attempt = 0; attempt < 60; attempt += 1) {
    try { await fetch(`http://127.0.0.1:${port}`); return; } catch { await new Promise((resolve) => setTimeout(resolve, 250)); }
  }
  throw new Error(`Mini App dev server did not start on ${port}`);
};

await mkdir(output, { recursive: true });
const servers = [start("buyer", 5173), start("merchant", 5174)];
try {
  await Promise.all(servers.map(({ port }) => waitFor(port)));
  const browser = await chromium.launch();
  const manifest = [];
  try {
    for (const app of ["buyer", "merchant"]) {
      const port = app === "buyer" ? 5173 : 5174;
      for (const item of matrix) {
        const page = await browser.newPage({ viewport: { width: item.width, height: 900 }, colorScheme: item.theme });
        const url = `http://127.0.0.1:${port}/${launchParams(item.theme, item.language)}`;
        await page.goto(url, { waitUntil: "networkidle" });
        const name = `${app}-${item.width}-${item.theme}-${item.language}.png`;
        await page.screenshot({ path: join(output, name), fullPage: true });
        manifest.push(name);
        await page.close();
      }
    }
  } finally { await browser.close(); }
  await writeFile(join(output, "manifest.txt"), `${manifest.join("\n")}\n`);
  console.log(`Captured ${manifest.length} Mini App screenshots in ${output}`);
} finally { for (const { child } of servers) child.kill("SIGTERM"); }
