import { chromium } from "../../website/node_modules/playwright/index.mjs";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";

const server = spawn("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", "5176", "--strictPort"], {
  cwd: new URL("../", import.meta.url).pathname,
  env: { ...process.env, VITE_ADMIN_API_MODE: "real" },
  stdio: "ignore"
});

const waitForServer = async () => {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch("http://127.0.0.1:5176/");
      if (response.ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error("admin dev server did not start");
};

await waitForServer();
const chromePath = process.env.CHROME_PATH ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const browser = await chromium.launch(existsSync(chromePath) ? { executablePath: chromePath } : {});
try {
  const page = await browser.newPage();
  await page.route(/\/v1\/admin\/auth\/refresh/, (route) => route.fulfill({ json: { data: { access_token: "e2e-token" } } }));
  await page.route(/\/v1\/admin\/auth\/login/, (route) => route.fulfill({ json: { data: { access_token: "e2e-token", must_change_password: false } } }));
  await page.route(/\/v1\/admin\/catalog\/products(?:\?.*)?$/, (route) => route.fulfill({ json: { data: { items: [{ public_id: "01ARZ3NDEKTSV4RRFFQ69G5FAV", name: { zh: "测试商品", en: "Test product", km: "ផលិតផលសាកល្បង" }, status: "review", updated_at: "2026-11-17T00:00:00Z" }] } } }));
  await page.route(/\/v1\/admin\/catalog\/products\/.*\/approve/, (route) => route.fulfill({ json: { data: {} } }));
  await page.route(/\/v1\/admin\/orders\?/, (route) => route.fulfill({ json: { data: { items: [{ public_id: "01ARZ3NDEKTSV4RRFFQ69G5FB4", payable_amount_e4: 125000, summary_status: "submitted", created_at: "2026-11-17T00:00:00Z", suborders: [{ public_id: "01ARZ3NDEKTSV4RRFFQ69G5FB5", merchant_public_id: "merchant-1", status: "submitted", items: [] }] }], next_cursor: null } } }));
  await page.route(/\/v1\/admin\/orders\/[^?]+$/, (route) => route.fulfill({ json: { data: { public_id: "01ARZ3NDEKTSV4RRFFQ69G5FB4", payable_amount_e4: 125000, summary_status: "submitted", created_at: "2026-11-17T00:00:00Z", shipping_address: { phone: "••••" }, suborders: [{ public_id: "01ARZ3NDEKTSV4RRFFQ69G5FB5", merchant_public_id: "merchant-1", status: "submitted", items: [] }] } } }));

  await page.goto("http://127.0.0.1:5176/login");
  await page.getByLabel("Email").fill("admin@example.test");
  await page.getByLabel("Password").fill("password");
  await page.getByLabel("Verification code").fill("123456");
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.goto("http://127.0.0.1:5176/review");
  await page.getByRole("tab", { name: "Product import review" }).click();
  await page.getByText("测试商品").waitFor();
  await page.getByRole("button", { name: "Approve" }).click();
  await page.goto("http://127.0.0.1:5176/orders");
  await page.getByText("01ARZ3NDEKTSV4RRFFQ69G5FB4").waitFor();
  await page.getByText("01ARZ3NDEKTSV4RRFFQ69G5FB4").click();
  await page.getByText("Order details").waitFor();
  console.log("Admin API Playwright flow passed");
} finally {
  await browser.close();
  server.kill("SIGTERM");
}
