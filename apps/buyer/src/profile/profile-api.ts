import type { Language } from "@drlife/i18n";

export type WalletEntryType = "payment" | "refund" | "topup" | "withdrawal";

export interface WalletEntry {
  id: string;
  type: WalletEntryType;
  amountE4: number;
  createdAt: string;
  orderPublicId?: string;
}

export interface BuyerProfile {
  displayName: string;
  username: string;
  orderCounts: { pending: number; completed: number };
  availableE4: number;
  frozenE4: number;
  entries: WalletEntry[];
}

export interface BuyerProfileApi {
  loadProfile(language: Language): Promise<BuyerProfile>;
}

/** The real adapter is intentionally a typed TODO until OpenAPI wallet fields are confirmed. */
export function createBuyerProfileApi(): BuyerProfileApi {
  return {
    async loadProfile() {
      if (import.meta.env.DEV) {
        const profile: BuyerProfile = {
          displayName: "Sokha Trading",
          username: "@sokha_demo",
          orderCounts: { pending: 2, completed: 8 },
          availableE4: 1250000,
          frozenE4: 300000,
          entries: [
            { id: "entry-1", type: "payment", amountE4: -125000, createdAt: "2026-11-15T08:00:00Z", orderPublicId: "ord_demo_001" },
            { id: "entry-2", type: "refund", amountE4: 50000, createdAt: "2026-11-13T10:00:00Z", orderPublicId: "ord_demo_000" }
          ]
        };
        return profile;
      }
      return { displayName: "", username: "", orderCounts: { pending: 0, completed: 0 }, availableE4: 0, frozenE4: 0, entries: [] };
    }
  };
}
