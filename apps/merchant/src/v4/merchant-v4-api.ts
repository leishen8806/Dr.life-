import type { Language } from "@drlife/i18n";

export type DemandState = "open" | "quoted" | "selected";
export interface MerchantDemand { id: string; title: string; state: DemandState; quantity: number; budgetE4: number; createdAt: string; }
export interface MerchantV4Profile { company: string; user: string; availableE4: number; pendingE4: number; permissions: string[]; demands: MerchantDemand[]; }
export interface WarehouseSnapshot { rateE4PerCbm: number; volumeCbm: number; pieces: number; measuredCbm?: number; storageFeeE4: number; }
export interface MerchantV4Api { loadProfile(language: Language): Promise<MerchantV4Profile>; loadWarehouse(language: Language): Promise<WarehouseSnapshot>; submitQuote(id: string, amountE4: number, deliveryDays: number, note: string): Promise<void>; }

const profile: MerchantV4Profile = {
  company: "Mekong Home",
  user: "Dara Sok",
  availableE4: 4200000,
  pendingE4: 950000,
  permissions: ["merchant.finance.view"],
  demands: [
    { id: "demand-1", title: "Ceramic wall tile", state: "open", quantity: 120, budgetE4: 2400000, createdAt: "2026-11-16T01:00:00Z" },
    { id: "demand-2", title: "LED panel", state: "quoted", quantity: 60, budgetE4: 1800000, createdAt: "2026-11-15T02:00:00Z" }
  ]
};
const warehouse: WarehouseSnapshot = { rateE4PerCbm: 125000, volumeCbm: 3.6, pieces: 42, measuredCbm: 3.4, storageFeeE4: 42500 };

export function createMerchantV4Api(): MerchantV4Api {
  return {
    async loadProfile() { return import.meta.env.DEV ? structuredClone(profile) : { ...profile, demands: [] }; },
    async loadWarehouse() { return import.meta.env.DEV ? structuredClone(warehouse) : { ...warehouse, pieces: 0, volumeCbm: 0, storageFeeE4: 0 }; },
    async submitQuote() { /* TODO: bind to the confirmed order-demand OpenAPI fields. */ }
  };
}
