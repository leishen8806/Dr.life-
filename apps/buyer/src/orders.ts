import type { LocalizedText } from "./types";

export type OrderStatus =
  | "pendingPayment"
  | "paid"
  | "submitted"
  | "accepted"
  | "fulfilling"
  | "shipped"
  | "delivered"
  | "settled"
  | "cancelled"
  | "rejected";

export interface OrderLine {
  publicId: string;
  name: LocalizedText;
  quantity: string;
  unit: string;
  unitPriceE4: bigint;
  totalE4: bigint;
}

export interface OrderEvent {
  label: OrderStatus;
  at: string;
}

export interface BuyerOrder {
  publicId: string;
  status: OrderStatus;
  createdAt: string;
  merchantName: LocalizedText;
  lines: readonly OrderLine[];
  totalE4: bigint;
  deliveryStatus: OrderStatus;
  events: readonly OrderEvent[];
}

function line(
  publicId: string,
  name: LocalizedText,
  quantity: string,
  unit: string,
  unitPriceE4: bigint
): OrderLine {
  const totalE4 = unitPriceE4 * BigInt(quantity);
  return { publicId, name, quantity, unit, unitPriceE4, totalE4 };
}

function order(
  publicId: string,
  status: OrderStatus,
  merchantName: LocalizedText,
  productName: LocalizedText,
  quantity: string,
  unitPriceE4: bigint,
  day: string
): BuyerOrder {
  const lines = [line(`${publicId}-line`, productName, quantity, "m2", unitPriceE4)];
  return {
    publicId,
    status,
    createdAt: `2026-11-${day}T09:30:00Z`,
    merchantName,
    lines,
    totalE4: lines.reduce((total, item) => total + item.totalE4, 0n),
    deliveryStatus: status,
    events: [{ label: status, at: `2026-11-${day}T09:30:00Z` }]
  };
}

const chinaMerchant = { zh: "金边建材直供", en: "Phnom Penh Materials", km: "សម្ភារៈសំណង់ភ្នំពេញ" };
const localMerchant = { zh: "本地家装供应", en: "Local Home Supply", km: "សម្ភារៈផ្ទះក្នុងស្រុក" };
const tile = { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" };
const paint = { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" };

/** Interface-stub data: deliberately covers every buyer-visible order state. */
export const mockOrders: readonly BuyerOrder[] = [
  order("ORD-1001", "pendingPayment", chinaMerchant, tile, "12", 95_000n, "01"),
  order("ORD-1002", "paid", localMerchant, paint, "4", 380_000n, "02"),
  order("ORD-1003", "submitted", chinaMerchant, tile, "100", 88_000n, "03"),
  order("ORD-1004", "accepted", localMerchant, paint, "6", 380_000n, "04"),
  order("ORD-1005", "fulfilling", chinaMerchant, tile, "20", 95_000n, "05"),
  order("ORD-1006", "shipped", localMerchant, paint, "8", 380_000n, "06"),
  order("ORD-1007", "delivered", chinaMerchant, tile, "50", 95_000n, "07"),
  order("ORD-1008", "settled", localMerchant, paint, "10", 380_000n, "08"),
  order("ORD-1009", "cancelled", chinaMerchant, tile, "1", 95_000n, "09"),
  order("ORD-1010", "rejected", localMerchant, paint, "2", 380_000n, "10")
];

export function findOrder(
  orders: readonly BuyerOrder[],
  publicId: string
): BuyerOrder | undefined {
  return orders.find((item) => item.publicId === publicId);
}
