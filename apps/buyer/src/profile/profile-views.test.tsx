import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t as translate, type Language } from "@drlife/i18n";
import { BuyerProfileView, BuyerWalletView } from "./profile-views";
import type { BuyerProfile, WalletEntry } from "./profile-api";

const profile: BuyerProfile = { displayName: "Sokha Trading", username: "@sokha_demo", orderCounts: { pending: 2, completed: 8 }, availableE4: 1250000, frozenE4: 300000, entries: [{ id: "entry", type: "payment", amountE4: -125000, createdAt: "2026-11-15T08:00:00Z" }] };
const translateFor = (language: Language) => (key: Parameters<typeof translate>[0], params?: Record<string, string | number>) => translate(key, params, language);
const noOp = () => undefined;

describe("buyer V4 profile views", () => {
  it.each(["zh", "en", "km"] as Language[])("renders profile and wallet in %s", (language) => {
    const profileMarkup = renderToStaticMarkup(<BuyerProfileView profile={{ status: "ready", value: profile }} language={language} t={translateFor(language)} onOpenWallet={noOp} onOpenOrders={noOp} />);
    const walletMarkup = renderToStaticMarkup(<BuyerWalletView entries={{ status: "ready", value: profile.entries }} language={language} t={translateFor(language)} onBack={noOp} />);
    expect(profileMarkup).toContain(profile.displayName);
    expect(walletMarkup).toContain(translate("wallet.title", {}, language));
  });

  it("keeps a wallet with no entries in the empty state", () => {
    const entries: WalletEntry[] = [];
    const markup = renderToStaticMarkup(<BuyerWalletView entries={{ status: "ready", value: entries }} language="en" t={translateFor("en")} onBack={noOp} />);
    expect(markup).toContain(translate("wallet.empty", {}, "en"));
  });
});
