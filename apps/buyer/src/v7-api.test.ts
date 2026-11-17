import { describe, expect, it } from "vitest";
import { createBuyerV7Api } from "./v7-api";

describe("buyer V7 adapter boundary", () => {
  it("exposes typed DEV mock operations", async () => {
    const api = createBuyerV7Api();
    await expect(api.submitInquiry({ companyId: "company", description: "kitchen", budget: "1000", duration: "30", region: "Kandal", contact: "telegram" })).resolves.toBeUndefined();
    await expect(api.listNotifications()).resolves.toHaveLength(1);
  });
});
