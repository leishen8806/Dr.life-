import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t as translate } from "@drlife/i18n";
import { MerchantV4View } from "./merchant-v4-view";
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
});
