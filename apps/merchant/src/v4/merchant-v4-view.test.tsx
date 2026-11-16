import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t as translate } from "@drlife/i18n";
import { MerchantV4View } from "./merchant-v4-view";
import { MerchantProfile, getDemandMetricCount } from "./merchant-v4-view";
import type { MerchantV4Api } from "./merchant-v4-api";

const api: MerchantV4Api = {
  loadProfile: async () => ({ company: "Mekong Home", user: "Dara Sok", availableE4: 4200000, pendingE4: 950000, permissions: ["merchant.finance.view"], demands: [] }),
  loadWarehouse: async () => ({ rateE4PerCbm: 125000, volumeCbm: 0, pieces: 0, storageFeeE4: 0 }),
  submitQuote: async () => undefined
};

describe("merchant V4 adapter boundary", () => {
  it("does not expose a write action for funds and keeps the inbound action disabled", () => {
    const markup = renderToStaticMarkup(<MerchantV4View api={api} language="en" t={(key, params) => translate(key, params, "en")} onBack={() => undefined} />);
    expect(markup).toContain(translate("common.loading", {}, "en"));
    expect(markup).not.toContain("Withdraw");
  });

  it("does not expose finance amounts without the finance permission", () => {
    const markup = renderToStaticMarkup(<MerchantProfile profile={{ ...apiProfile(), permissions: [] }} hasFinance={false} language="en" t={(key, params) => translate(key, params, "en")} onDemand={() => undefined} />);
    expect(markup).not.toContain('data-e4="4200000"');
    expect(markup).not.toContain('data-e4="950000"');
    expect(markup).not.toContain(translate("merchant.funds", {}, "en"));
  });

  it("exposes finance amounts only with the finance permission", () => {
    const markup = renderToStaticMarkup(<MerchantProfile profile={apiProfile()} hasFinance language="en" t={(key, params) => translate(key, params, "en")} onDemand={() => undefined} />);
    expect(markup).toContain(translate("merchant.funds", {}, "en"));
    expect(markup).toContain('data-e4="4200000"');
    expect(markup).toContain('data-e4="950000"');
  });

  it("derives demand metrics from states and hides unavailable states", () => {
    const demands = [
      { id: "open-1", title: "Open", state: "open" as const, quantity: 1, budgetE4: 10000, createdAt: "2026-11-16" },
      { id: "open-2", title: "Open 2", state: "open" as const, quantity: 1, budgetE4: 10000, createdAt: "2026-11-16" },
      { id: "payment-1", title: "Payment", state: "awaitingPayment" as const, quantity: 1, budgetE4: 10000, createdAt: "2026-11-16" }
    ];
    expect(getDemandMetricCount(demands, "needQuote")).toBe(2);
    expect(getDemandMetricCount(demands, "awaitingPayment")).toBe(1);
    expect(getDemandMetricCount(demands, "awaitingShipment")).toBeUndefined();
  });
});

function apiProfile() {
  return { company: "Mekong Home", user: "Dara Sok", availableE4: 4200000, pendingE4: 950000, permissions: ["merchant.finance.view"], demands: [] };
}
