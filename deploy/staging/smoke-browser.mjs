import { chromium } from "playwright";

function option(name) {
  const index = process.argv.indexOf(name);
  const value = index >= 0 ? process.argv[index + 1] : undefined;
  if (!value) throw new Error(`${name} is required`);
  return value;
}

const label = option("--label");
const url = option("--url");
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();
const errors = [];
const failedResources = [];

page.on("console", (message) => {
  if (message.type() === "error") errors.push(`console: ${message.text()}`);
});
page.on("pageerror", (error) => errors.push(`pageerror: ${error.message}`));
page.on("requestfailed", (request) => failedResources.push(`${request.url()}: ${request.failure()?.errorText ?? "failed"}`));

try {
  const response = await page.goto(url, { waitUntil: "networkidle", timeout: 30000 });
  if (!response || response.status() >= 400) throw new Error(`document returned HTTP ${response?.status() ?? "no response"}`);
  const render = await page.evaluate(() => {
    const scripts = [...document.querySelectorAll("script[src]")];
    const body = document.body;
    const rect = body?.getBoundingClientRect();
    return {
      mainScriptCount: scripts.length,
      visibleText: body?.innerText.trim().length ?? 0,
      bodyVisible: Boolean(rect && rect.width > 0 && rect.height > 0)
    };
  });
  if (render.mainScriptCount === 0) throw new Error("document did not load a main JavaScript bundle");
  if (render.visibleText === 0 || !render.bodyVisible) throw new Error("document rendered no visible text");
  if (errors.length || failedResources.length) throw new Error([...errors, ...failedResources].join("; "));
  console.log(`PASS: ${label} browser render (${render.visibleText} visible characters)`);
} finally {
  await browser.close();
}
