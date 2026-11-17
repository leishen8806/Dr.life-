import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t as translate } from "@drlife/i18n";
import { MerchantV7View, canWriteMerchant, validateCompany } from "./v7-view";

const t = (key: Parameters<typeof translate>[0], params?: Record<string, string | number>) => translate(key, params, "en");

describe("merchant V7 pages", () => {
  it("validates company profile required fields", () => {
    expect(validateCompany({ name: "", address: "", category: "", employees: "", stores: "", city: "" }, t)).toHaveLength(4);
    expect(validateCompany({ name: "Mekong", address: "Kandal", category: "Materials", employees: "8", stores: "1", city: "Phnom Penh" }, t)).toEqual([]);
  });

  it("keeps merchant writes behind the development adapter boundary", () => {
    expect(canWriteMerchant(false)).toBe(false);
    expect(canWriteMerchant(true)).toBe(true);
  });

  it("renders the business dashboard and unavailable placeholders", () => {
    const markup = renderToStaticMarkup(<MerchantV7View language="en" t={t} onBack={() => undefined} />);
    expect(markup).toContain(t("merchant.dashboardTitle"));
    expect(markup).toContain(t("merchant.warehouseEntry"));
    expect(markup).toContain(t("merchant.aiAssistant"));
    expect(markup).toContain(t("common.comingSoon"));
  });
});
