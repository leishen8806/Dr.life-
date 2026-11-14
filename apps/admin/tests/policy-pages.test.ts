import { beforeEach, describe, expect, it } from "vitest";
import { mockApi } from "../src/api/mock";

describe("agreement and integration adapters", () => {
  beforeEach(async () => {
    const rows = await mockApi.listAgreements();
    await mockApi.saveAgreementDraft(rows[1].id, "zh", "示例条款（模拟数据，非正式法律文本）");
    await mockApi.saveAgreementDraft(rows[1].id, "en", "Example terms (mock data, not formal legal text)");
    await mockApi.saveAgreementDraft(rows[1].id, "km", "គំរូលក្ខខណ្ឌ (ទិន្នន័យសាកល្បង មិនមែនអត្ថបទច្បាប់ផ្លូវការ)");
  });

  it("requires all three language drafts, change note, and effective date before publish", async () => {
    const rows = await mockApi.listAgreements();
    await expect(mockApi.publishAgreement(rows[1].id, { changeNote: "", effectiveDate: "" })).rejects.toThrow("agreement_incomplete");
    const published = await mockApi.publishAgreement(rows[1].id, { changeNote: "Editorial update", effectiveDate: "2026-11-14" });
    expect(published.state).toBe("published");
  });

  it("only returns the last four secret characters and clears enable state after replacement", async () => {
    const rows = await mockApi.listIntegrations();
    const updated = await mockApi.updateIntegration(rows[0].id, { endpoint: "https://maps.example.invalid", environment: "test", secret: "do-not-store-this-secret" });
    expect(updated.secretLast4).toBe("cret");
    expect(JSON.stringify(updated)).not.toContain("do-not-store-this-secret");
    await expect(mockApi.enableIntegration(updated.id)).rejects.toThrow("integration_not_tested");
    await mockApi.testIntegration(updated.id);
    expect((await mockApi.enableIntegration(updated.id)).enabled).toBe(true);
  });
});
