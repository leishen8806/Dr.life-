import type { LocalizedText } from "./types";

export type OrderStatus =
  | "awaiting_payment"
  | "paid"
  | "submitted"
  | "accepted"
  | "in_fulfillment"
  | "shipped"
  | "delivered_verified"
  | "settled"
  | "cancelled"
  | "rejected"
  | "delivery_failed"
  | "returning"
  | "returned"
  | "exception";

export type PaymentStatus = "pending" | "confirming" | "succeeded" | "failed" | "refunded";

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

export interface BuyerSubOrder {
  publicId: string;
  merchantName: LocalizedText;
  status: OrderStatus;
  paymentStatus: PaymentStatus;
  lines: readonly OrderLine[];
  totalE4: bigint;
  events: readonly OrderEvent[];
}

export interface BuyerOrder {
  publicId: string;
  createdAt: string;
  totalE4: bigint;
  subOrders: readonly BuyerSubOrder[];
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

function subOrder(
  publicId: string,
  status: OrderStatus,
  paymentStatus: PaymentStatus,
  merchantName: LocalizedText,
  productName: LocalizedText,
  quantity: string,
  unitPriceE4: bigint,
  day: string
): BuyerSubOrder {
  const lines = [line(`${publicId}-line`, productName, quantity, "m2", unitPriceE4)];
  return {
    publicId,
    status,
    paymentStatus,
    merchantName,
    lines,
    totalE4: lines.reduce((total, item) => total + item.totalE4, 0n),
    events: [{ label: status, at: `2026-11-${day}T09:30:00Z` }]
  };
}

function order(publicId: string, day: string, subOrders: readonly BuyerSubOrder[]): BuyerOrder {
  return {
    publicId,
    createdAt: `2026-11-${day}T09:30:00Z`,
    totalE4: subOrders.reduce((total, item) => total + item.totalE4, 0n),
    subOrders
  };
}

const chinaMerchant = { zh: "金边建材直供", en: "Phnom Penh Materials", km: "សម្ភារៈសំណង់ភ្នំពេញ" };
const localMerchant = { zh: "本地家装供应", en: "Local Home Supply", km: "សម្ភារៈផ្ទះក្នុងស្រុក" };
const tile = { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" };
const paint = { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" };

/** Interface-stub data: deliberately covers every buyer-visible order state. */
export const mockOrders: readonly BuyerOrder[] = [
  order("ORD-1001", "01", [subOrder("SUB-1001-CN", "awaiting_payment", "pending", chinaMerchant, tile, "12", 95_000n, "01")]),
  order("ORD-1002", "02", [subOrder("SUB-1002-LC", "paid", "succeeded", localMerchant, paint, "4", 380_000n, "02")]),
  order("ORD-1003", "03", [subOrder("SUB-1003-CN", "submitted", "succeeded", chinaMerchant, tile, "100", 88_000n, "03")]),
  order("ORD-1004", "04", [subOrder("SUB-1004-LC", "accepted", "succeeded", localMerchant, paint, "6", 380_000n, "04")]),
  order("ORD-1005", "05", [subOrder("SUB-1005-CN", "in_fulfillment", "succeeded", chinaMerchant, tile, "20", 95_000n, "05")]),
  order("ORD-1006", "06", [subOrder("SUB-1006-LC", "shipped", "succeeded", localMerchant, paint, "8", 380_000n, "06")]),
  order("ORD-1007", "07", [subOrder("SUB-1007-CN", "delivered_verified", "succeeded", chinaMerchant, tile, "50", 95_000n, "07")]),
  order("ORD-1008", "08", [subOrder("SUB-1008-LC", "settled", "succeeded", localMerchant, paint, "10", 380_000n, "08")]),
  order("ORD-1009", "09", [subOrder("SUB-1009-CN", "cancelled", "refunded", chinaMerchant, tile, "1", 95_000n, "09")]),
  order("ORD-1010", "10", [subOrder("SUB-1010-LC", "rejected", "failed", localMerchant, paint, "2", 380_000n, "10")]),
  order("ORD-1011", "11", [
    subOrder("SUB-1011-CN", "paid", "confirming", chinaMerchant, tile, "2", 95_000n, "11"),
    subOrder("SUB-1011-LC", "submitted", "confirming", localMerchant, paint, "1", 380_000n, "11")
  ]),
  order("ORD-1012", "12", [subOrder("SUB-1012-CN", "delivery_failed", "succeeded", chinaMerchant, tile, "3", 95_000n, "12")]),
  order("ORD-1013", "13", [subOrder("SUB-1013-LC", "returning", "succeeded", localMerchant, paint, "3", 380_000n, "13")]),
  order("ORD-1014", "14", [subOrder("SUB-1014-CN", "returned", "refunded", chinaMerchant, tile, "2", 95_000n, "14")]),
  order("ORD-1015", "15", [subOrder("SUB-1015-LC", "exception", "succeeded", localMerchant, paint, "2", 380_000n, "15")])
];

export function findOrder(
  orders: readonly BuyerOrder[],
  publicId: string
): BuyerOrder | undefined {
  return orders.find((item) => item.publicId === publicId);
}
