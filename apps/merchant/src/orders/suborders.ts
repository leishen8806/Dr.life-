import type { Language } from "@drlife/i18n";

export type SuborderStatus =
  | "awaiting_payment"
  | "paid"
  | "submitted"
  | "accepted"
  | "in_fulfillment"
  | "shipped"
  | "delivered_verified"
  | "settled"
  | "rejected"
  | "cancelled"
  | "delivery_failed"
  | "returning"
  | "returned"
  | "exception";

type LocalizedText = Record<Language, string>;

export interface MerchantSuborder {
  publicId: string;
  status: SuborderStatus;
  buyerName: LocalizedText;
  productName: LocalizedText;
  quantity: string;
  unit: string;
  unitPriceE4: bigint;
  totalE4: bigint;
}

export const mockSuborders: readonly MerchantSuborder[] = [
  { publicId: "SUB-2001", status: "awaiting_payment", buyerName: { zh: "买家甲", en: "Buyer A", km: "អ្នកទិញ ក" }, productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "12", unit: "m2", unitPriceE4: 95_000n, totalE4: 1_140_000n },
  { publicId: "SUB-2002", status: "paid", buyerName: { zh: "买家乙", en: "Buyer B", km: "អ្នកទិញ ខ" }, productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "4", unit: "pail", unitPriceE4: 380_000n, totalE4: 1_520_000n },
  { publicId: "SUB-2003", status: "submitted", buyerName: { zh: "买家丙", en: "Buyer C", km: "អ្នកទិញ គ" }, productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "100", unit: "m2", unitPriceE4: 88_000n, totalE4: 8_800_000n },
  { publicId: "SUB-2004", status: "accepted", buyerName: { zh: "买家丁", en: "Buyer D", km: "អ្នកទិញ ឃ" }, productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "6", unit: "pail", unitPriceE4: 380_000n, totalE4: 2_280_000n },
  { publicId: "SUB-2005", status: "in_fulfillment", buyerName: { zh: "买家戊", en: "Buyer E", km: "អ្នកទិញ ង" }, productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "20", unit: "m2", unitPriceE4: 95_000n, totalE4: 1_900_000n },
  { publicId: "SUB-2006", status: "shipped", buyerName: { zh: "买家己", en: "Buyer F", km: "អ្នកទិញ ច" }, productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "8", unit: "pail", unitPriceE4: 380_000n, totalE4: 3_040_000n },
  { publicId: "SUB-2007", status: "delivered_verified", buyerName: { zh: "买家庚", en: "Buyer G", km: "អ្នកទិញ ជ" }, productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "50", unit: "m2", unitPriceE4: 95_000n, totalE4: 4_750_000n },
  { publicId: "SUB-2008", status: "settled", buyerName: { zh: "买家辛", en: "Buyer H", km: "អ្នកទិញ ឈ" }, productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "10", unit: "pail", unitPriceE4: 380_000n, totalE4: 3_800_000n },
  { publicId: "SUB-2009", status: "rejected", buyerName: { zh: "买家壬", en: "Buyer I", km: "អ្នកទិញ ញ" }, productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "1", unit: "m2", unitPriceE4: 95_000n, totalE4: 95_000n },
  { publicId: "SUB-2010", status: "cancelled", buyerName: { zh: "买家癸", en: "Buyer J", km: "អ្នកទិញ ដ" }, productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "2", unit: "pail", unitPriceE4: 380_000n, totalE4: 760_000n },
  { publicId: "SUB-2011", status: "delivery_failed", buyerName: { zh: "买家子", en: "Buyer K", km: "អ្នកទិញ ត" }, productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "3", unit: "m2", unitPriceE4: 95_000n, totalE4: 285_000n },
  { publicId: "SUB-2012", status: "returning", buyerName: { zh: "买家丑", en: "Buyer L", km: "អ្នកទិញ ថ" }, productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "3", unit: "pail", unitPriceE4: 380_000n, totalE4: 1_140_000n },
  { publicId: "SUB-2013", status: "returned", buyerName: { zh: "买家寅", en: "Buyer M", km: "អ្នកទិញ ទ" }, productName: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" }, quantity: "2", unit: "m2", unitPriceE4: 95_000n, totalE4: 190_000n },
  { publicId: "SUB-2014", status: "exception", buyerName: { zh: "买家卯", en: "Buyer N", km: "អ្នកទិញ ន" }, productName: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" }, quantity: "2", unit: "pail", unitPriceE4: 380_000n, totalE4: 760_000n }
];

export const suborderStatuses: readonly SuborderStatus[] = [
  "awaiting_payment",
  "paid",
  "submitted",
  "accepted",
  "in_fulfillment",
  "shipped",
  "delivered_verified",
  "settled",
  "rejected",
  "cancelled",
  "delivery_failed",
  "returning",
  "returned",
  "exception"
];
