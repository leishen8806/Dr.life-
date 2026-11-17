import type { Language } from "@drlife/i18n";

export type DemandState = "open" | "quoted" | "selected" | "selectedByOthers" | "awaitingPayment" | "awaitingShipment" | "inDelivery";
export interface MerchantDemand { id: string; title: string; state: DemandState; quantity: number; budgetE4: number; createdAt: string; }
export interface MerchantFinanceEntry { id: string; type: "settlement" | "withdrawal"; amountE4: number; createdAt: string; detail: string; }
export interface MerchantV4Profile { company: string; user: string; availableE4: number; pendingE4: number; permissions: string[]; demands: MerchantDemand[]; financeEntries?: MerchantFinanceEntry[]; }
export interface InboundBookingInput { productId: string; quantity: number; method: "self" | "pickup" | "china"; }
export interface WarehouseSnapshot { rateE4PerCbm: number; volumeCbm: number; pieces: number; measuredCbm?: number; storageFeeE4: number; productIds: string[]; }
export interface MerchantV4Api { loadProfile(language: Language): Promise<MerchantV4Profile>; loadWarehouse(language: Language): Promise<WarehouseSnapshot>; submitQuote(id: string, amountE4: number, deliveryDays: number, quoteExpiresAt: string, note: string): Promise<void>; submitInbound(input: InboundBookingInput): Promise<void>; }

export function createMerchantV4Api(): MerchantV4Api {
  return {
    async loadProfile() {
      if (import.meta.env.DEV) return { company: "Mekong Home", user: "Dara Sok", availableE4: 4200000, pendingE4: 950000, permissions: ["merchant.finance.view"], demands: [{ id: "demand-1", title: "Ceramic wall tile", state: "open" as const, quantity: 120, budgetE4: 2400000, createdAt: "2026-11-16T01:00:00Z" }, { id: "demand-2", title: "LED panel", state: "quoted" as const, quantity: 60, budgetE4: 1800000, createdAt: "2026-11-15T02:00:00Z" }, { id: "demand-3", title: "Wood flooring", state: "selectedByOthers" as const, quantity: 80, budgetE4: 1600000, createdAt: "2026-11-14T02:00:00Z" }], financeEntries: [{ id: "settlement-1", type: "settlement" as const, amountE4: 1200000, createdAt: "2026-11-15T03:00:00Z", detail: "Order settlement" }] };
      return { company: "", user: "", availableE4: 0, pendingE4: 0, permissions: [], demands: [] };
    },
    async loadWarehouse() {
      if (import.meta.env.DEV) return { rateE4PerCbm: 125000, volumeCbm: 3.6, pieces: 42, measuredCbm: 3.4, storageFeeE4: 42500, productIds: ["ceramicTile", "ledPanel"] };
      return { rateE4PerCbm: 0, volumeCbm: 0, pieces: 0, storageFeeE4: 0, productIds: [] };
    },
    async submitQuote() { /* TODO: bind to the confirmed order-demand OpenAPI fields, including quote expiry. */ },
    async submitInbound() { /* TODO: bind to the confirmed warehouse OpenAPI fields. */ }
  };
}
