import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

// Playwright is intentionally scoped to the website workspace.  Resolve it
// from that package instead of adding a duplicate root dependency or relying
// on NODE_PATH (which Node's ESM resolver does not use here).
const websitePackage = fileURLToPath(new URL("../../apps/website/package.json", import.meta.url));
const requireWebsite = createRequire(websitePackage);
const { chromium } = requireWebsite("playwright");

if (process.argv.includes("--self-check")) {
  console.log("PASS: staging browser smoke resolved package-local playwright");
  process.exit(0);
}

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
let expectedAuthFailures = 0;
const authResponses = [];

page.on("console", (message) => {
  if (message.type() !== "error") return;
  if (message.text().includes("Failed to load resource") && expectedAuthFailures > 0) {
    expectedAuthFailures -= 1;
    return;
  }
  errors.push(`console: ${message.text()}`);
});
page.on("pageerror", (error) => errors.push(`pageerror: ${error.message}`));
page.on("requestfailed", (request) => failedResources.push(`${request.url()}: ${request.failure()?.errorText ?? "failed"}`));
page.on("response", (response) => {
  if ((response.status() === 401 || response.status() === 403) && /\/auth\/(?:telegram|refresh|login)(?:$|\?)/.test(response.url())) {
    authResponses.push({url: response.url(), status: response.status()});
    if (response.status() === 401) expectedAuthFailures += 1;
  }
});

try {
  const response = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30000 });
  if (!response || response.status() >= 400) throw new Error(`document returned HTTP ${response?.status() ?? "no response"}`);
  await page.waitForTimeout(1000);
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
  const adminAccessFailure = authResponses.some(({url, status}) => status === 403 && /\/v1\/admin\/auth\/refresh(?:$|\?)/.test(url));
  if (adminAccessFailure) {
    throw new Error("admin refresh returned 403: AdminAccess allowlist or CSRF policy rejected the request; configure TRUSTED_PROXY_* and owner-provided ADMIN_ALLOWED_CIDRS before treating refresh as PASS");
  }
  if (errors.length || failedResources.length) throw new Error([...errors, ...failedResources].join("; "));
  console.log(`PASS: ${label} browser render (${render.visibleText} visible characters)`);
} finally {
  await browser.close();
}
