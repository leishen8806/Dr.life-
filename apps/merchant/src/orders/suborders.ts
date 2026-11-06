import type { Language } from "@drlife/i18n";

export type SuborderStatus = "pendingPayment" | "paid" | "shipped" | "delivered";

export interface MerchantSuborder {
  publicId: string;
  status: SuborderStatus;
  buyerName: string;
  productName: Record<Language, string>;
  quantity: string;
  unit: string;
  unitPriceE4: bigint;
  totalE4: bigint;
  deliveryStatus: string;
}

export const mockSuborders: readonly MerchantSuborder[] = [
  { publicId: "SUB-2001", status: "pendingPayment", buyerName: "Buyer A", productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "12", unit: "m2", unitPriceE4: 95_000n, totalE4: 1_140_000n, deliveryStatus: "awaiting_payment" },
  { publicId: "SUB-2002", status: "paid", buyerName: "Buyer B", productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "4", unit: "pail", unitPriceE4: 380_000n, totalE4: 1_520_000n, deliveryStatus: "ready_to_fulfil" },
  { publicId: "SUB-2003", status: "shipped", buyerName: "Buyer C", productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "100", unit: "m2", unitPriceE4: 88_000n, totalE4: 8_800_000n, deliveryStatus: "in_transit" },
  { publicId: "SUB-2004", status: "delivered", buyerName: "Buyer D", productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "6", unit: "pail", unitPriceE4: 380_000n, totalE4: 2_280_000n, deliveryStatus: "delivered" }
];

export const suborderStatuses: readonly SuborderStatus[] = [
  "pendingPayment",
  "paid",
  "shipped",
  "delivered"
];
