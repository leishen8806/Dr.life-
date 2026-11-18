import { describe, expect, it } from "vitest";
import { createMerchantV7Api } from "./v7-api";

describe("merchant V7 adapter boundary", () => {
  it("keeps DEV writes behind a typed adapter", async () => {
    const api = createMerchantV7Api();
    await expect(api.importProducts({ source: "csv", rightsConfirmed: true })).resolves.toEqual({ status: "pending_review" });
    await expect(api.submitOnboarding({ companyName: "Mekong", contact: "Dara", phone: "012", licenseFileName: "license.pdf", category: "Materials" })).resolves.toEqual({ status: "pending_review" });
  });
});
