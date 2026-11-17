import { test, expect } from "../../website/node_modules/playwright/test.js";

test.describe("admin login and one-time administrator flow", () => {
  test.beforeEach(async ({ page }) => {
    await page.route("**/v1/admin/auth/login", async (route) => route.fulfill({ json: { data: { access_token: "memory-only", must_change_password: false } } }));
    await page.route("**/v1/admin/admins", async (route) => {
      if (route.request().method() === "POST") return route.fulfill({ json: { data: { admin: { public_id: "admin-2", name: "New Operator", email: "operator@example.test", role: "ops", data_scope: ["KH-PP"], status: "active", created_at: "2026-11-17" }, initial_password: "one-time-password", totp_provisioning_uri: "otpauth://example.test/fake" } } });
      return route.fulfill({ json: { data: { items: [], next_cursor: null } } });
    });
    await page.route("**/v1/admin/auth/step-up", async (route) => route.fulfill({ json: { data: { step_up_token: "step-up-token" } } }));
  });

  test("logs in, opens administrator page, creates an admin, and logs out", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill("admin@example.test");
    await page.getByLabel("Password").fill("password");
    await page.getByLabel("Verification code").fill("123456");
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByText("Dr.life+")).toBeVisible();
    await page.goto("/administrators");
    await page.getByRole("button", { name: "New administrator" }).click();
    await page.getByLabel("Name").fill("New Operator");
    await page.getByLabel("Email").fill("operator@example.test");
    await page.getByRole("combobox").click();
    await page.getByText("ops", { exact: true }).click();
    await page.getByLabel("Verification code").fill("123456");
    await page.getByRole("button", { name: "New administrator" }).last().click();
    await expect(page.getByText("one-time-password")).toBeVisible();
    await page.getByRole("button", { name: "I saved these credentials" }).click();
    await expect(page.getByText("one-time-password")).toHaveCount(0);
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
  });
});
