import { createServer } from "node:http";
import { chromium } from "../../website/node_modules/playwright/index.mjs";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";

const adminOrigin = "http://127.0.0.1:5176";
const apiOrigin = "http://127.0.0.1:5177";
const credentialPaths = new Set(["/v1/admin/auth/login", "/v1/admin/auth/refresh"]);

function sendJson(response, status, body, path) {
  response.setHeader("Access-Control-Allow-Origin", adminOrigin);
  response.setHeader("Vary", "Origin");
  if (credentialPaths.has(path)) response.setHeader("Access-Control-Allow-Credentials", "true");
  response.setHeader("Content-Type", "application/json");
  response.writeHead(status);
  response.end(JSON.stringify(body));
}

const api = createServer((request, response) => {
  const path = new URL(request.url, apiOrigin).pathname;
  if (request.method === "OPTIONS") {
    response.setHeader("Access-Control-Allow-Origin", adminOrigin);
    response.setHeader("Access-Control-Allow-Methods", "GET,POST,OPTIONS");
    response.setHeader("Access-Control-Allow-Headers", "content-type,authorization,x-requested-with");
    response.setHeader("Vary", "Origin");
    if (credentialPaths.has(path)) response.setHeader("Access-Control-Allow-Credentials", "true");
    response.writeHead(204);
    response.end();
    return;
  }
  if (request.method === "POST" && path === "/v1/admin/auth/login") {
    response.setHeader("Set-Cookie", "refresh=e2e-refresh; Path=/; SameSite=Strict");
    sendJson(response, 200, { data: { access_token: "cors-token", must_change_password: false } }, path);
    return;
  }
  if (request.method === "POST" && path === "/v1/admin/auth/refresh") {
    sendJson(response, 200, { data: { access_token: "cors-token" } }, path);
    return;
  }
  if (!request.headers.authorization) {
    sendJson(response, 401, { error: { code: "unauthorized" } }, path);
    return;
  }
  if (request.method === "GET" && path === "/v1/admin/admins") {
    sendJson(response, 200, { data: { items: [{ public_id: "admin-1", name: "跨域管理员", email: "admin@example.test", role: "super", data_scope: ["KH-PP"], status: "active", created_at: "2026-11-20" }], next_cursor: null } }, path);
    return;
  }
  if (request.method === "GET" && path === "/v1/admin/audit") {
    sendJson(response, 200, { data: { items: [{ id: 1, ts: "2026-11-20T00:00:00Z", actor_id: 1, actor_role: "super", action: "auth.login", target: "admin:admin-1", detail: {}, request_id: "cors-e2e" }], next_before_id: null } }, path);
    return;
  }
  sendJson(response, 404, { error: { code: "not_found" } }, path);
});

await new Promise((resolve) => api.listen(5177, "127.0.0.1", resolve));
const app = spawn("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", "5176", "--strictPort"], {
  cwd: new URL("../", import.meta.url).pathname,
  env: { ...process.env, VITE_ADMIN_API_MODE: "real", VITE_API_BASE_URL: apiOrigin },
  stdio: "ignore"
});

const waitForServer = async () => {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    try {
      const result = await fetch(`${adminOrigin}/`);
      if (result.ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error("admin dev server did not start");
};

await waitForServer();
const chromePath = process.env.CHROME_PATH;
const browser = await chromium.launch(chromePath && existsSync(chromePath) ? { executablePath: chromePath } : {});
try {
  const page = await browser.newPage();
  const consoleErrors = [];
  page.on("console", (message) => { if (message.type() === "error") consoleErrors.push(message.text()); });
  page.on("pageerror", (error) => consoleErrors.push(error.message));
  await page.goto(`${adminOrigin}/login`);
  await page.getByLabel("Email").fill("admin@example.test");
  await page.getByLabel("Password").fill("password");
  await page.getByLabel("Verification code").fill("123456");
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.goto(`${adminOrigin}/administrators`);
  await page.getByText("跨域管理员").waitFor();
  await page.goto(`${adminOrigin}/audit`);
  await page.getByText("auth.login").waitFor();
  const corsErrors = consoleErrors.filter((message) => /cors|access-control/i.test(message));
  if (corsErrors.length > 0) throw new Error(`cross-origin browser errors: ${corsErrors.join(" | ")}`);
  console.log("Admin cross-origin CORS Playwright flow passed");
} finally {
  await browser.close();
  app.kill("SIGTERM");
  await new Promise((resolve) => api.close(resolve));
}
