import type { BuyerOrder, BuyerSubOrder } from "./orders";

export function isShippingQuotePending(subOrder: BuyerSubOrder, now: number): boolean {
  return subOrder.status === "awaiting_payment" &&
    (!subOrder.shippingQuoteExpiresAt || Date.parse(subOrder.shippingQuoteExpiresAt) <= now);
}

export function isOrderQuotePending(order: BuyerOrder, now: number): boolean {
  return order.subOrders.some((subOrder) => isShippingQuotePending(subOrder, now));
}
