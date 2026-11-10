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
  lines: readonly OrderLine[];
  totalE4: bigint;
  shippingAmountE4: bigint;
  shippingQuoteExpiresAt: string;
  events: readonly OrderEvent[];
}

export interface BuyerOrder {
  publicId: string;
  createdAt: string;
  totalE4: bigint;
  paymentStatus: PaymentStatus;
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
  merchantName: LocalizedText,
  productName: LocalizedText,
  quantity: string,
  unitPriceE4: bigint,
  day: string,
  shippingAmountE4 = 0n
): BuyerSubOrder {
  const lines = [line(`${publicId}-line`, productName, quantity, "m2", unitPriceE4)];
  return {
    publicId,
    status,
    merchantName,
    lines,
    totalE4: lines.reduce((total, item) => total + item.totalE4, 0n),
    shippingAmountE4,
    shippingQuoteExpiresAt: `2026-11-${day}T10:30:00Z`,
    events: [{ label: status, at: `2026-11-${day}T09:30:00Z` }]
  };
}

function order(publicId: string, day: string, subOrders: readonly BuyerSubOrder[]): BuyerOrder {
  return {
    publicId,
    createdAt: `2026-11-${day}T09:30:00Z`,
    totalE4: subOrders.reduce((total, item) => total + item.totalE4, 0n),
    paymentStatus: "succeeded",
    subOrders
  };
}

function paymentOrder(publicId: string, day: string, paymentStatus: PaymentStatus, subOrders: readonly BuyerSubOrder[]): BuyerOrder {
  return { ...order(publicId, day, subOrders), paymentStatus };
}

const chinaMerchant = { zh: "金边建材直供", en: "Phnom Penh Materials", km: "សម្ភារៈសំណង់ភ្នំពេញ" };
const localMerchant = { zh: "本地家装供应", en: "Local Home Supply", km: "សម្ភារៈផ្ទះក្នុងស្រុក" };
const tile = { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿងប៉ូលា 800×800" };
const paint = { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" };

/** Interface-stub data: deliberately covers every buyer-visible order state. */
export const mockOrders: readonly BuyerOrder[] = [
  paymentOrder("ORD-1001", "01", "pending", [subOrder("SUB-1001-CN", "awaiting_payment", chinaMerchant, tile, "12", 95_000n, "01")]),
  order("ORD-1002", "02", [subOrder("SUB-1002-LC", "paid", localMerchant, paint, "4", 380_000n, "02")]),
  order("ORD-1003", "03", [subOrder("SUB-1003-CN", "submitted", chinaMerchant, tile, "100", 88_000n, "03")]),
  order("ORD-1004", "04", [subOrder("SUB-1004-LC", "accepted", localMerchant, paint, "6", 380_000n, "04")]),
  order("ORD-1005", "05", [subOrder("SUB-1005-CN", "in_fulfillment", chinaMerchant, tile, "20", 95_000n, "05")]),
  order("ORD-1006", "06", [subOrder("SUB-1006-LC", "shipped", localMerchant, paint, "8", 380_000n, "06")]),
  order("ORD-1007", "07", [subOrder("SUB-1007-CN", "delivered_verified", chinaMerchant, tile, "50", 95_000n, "07")]),
  order("ORD-1008", "08", [subOrder("SUB-1008-LC", "settled", localMerchant, paint, "10", 380_000n, "08")]),
  order("ORD-1009", "09", [subOrder("SUB-1009-CN", "cancelled", chinaMerchant, tile, "1", 95_000n, "09")]),
  paymentOrder("ORD-1010", "10", "refunded", [subOrder("SUB-1010-LC", "rejected", localMerchant, paint, "2", 380_000n, "10")]),
  paymentOrder("ORD-1011", "11", "confirming", [
    subOrder("SUB-1011-CN", "awaiting_payment", chinaMerchant, tile, "2", 95_000n, "11"),
    subOrder("SUB-1011-LC", "awaiting_payment", localMerchant, paint, "1", 380_000n, "11")
  ]),
  order("ORD-1012", "12", [subOrder("SUB-1012-CN", "delivery_failed", chinaMerchant, tile, "3", 95_000n, "12")]),
  order("ORD-1013", "13", [subOrder("SUB-1013-LC", "returning", localMerchant, paint, "3", 380_000n, "13")]),
  order("ORD-1014", "14", [subOrder("SUB-1014-CN", "returned", chinaMerchant, tile, "2", 95_000n, "14")]),
  order("ORD-1015", "15", [subOrder("SUB-1015-LC", "exception", localMerchant, paint, "2", 380_000n, "15")]),
  order("ORD-1016", "16", [
    subOrder("SUB-1016-CN", "shipped", chinaMerchant, tile, "2", 95_000n, "16"),
    subOrder("SUB-1016-LC", "in_fulfillment", localMerchant, paint, "1", 380_000n, "16")
  ]),
  paymentOrder("ORD-1017", "17", "failed", [subOrder("SUB-1017-CN", "awaiting_payment", chinaMerchant, tile, "3", 95_000n, "17", 12_500n)]),
];

export function payableAmountE4(subOrder: BuyerSubOrder): bigint {
  return subOrder.totalE4 + subOrder.shippingAmountE4;
}

export function findOrder(
  orders: readonly BuyerOrder[],
  publicId: string
): BuyerOrder | undefined {
  return orders.find((item) => item.publicId === publicId);
}
