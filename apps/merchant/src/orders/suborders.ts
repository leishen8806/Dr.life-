import type { components } from "@drlife/api-client";

export type SuborderStatus = components["schemas"]["OrderStatus"];
export type MerchantSuborder = components["schemas"]["MerchantSuborder"];

export const suborderStatuses: readonly SuborderStatus[] = [
  "awaiting_payment", "paid", "submitted", "accepted", "in_fulfillment",
  "shipped", "delivered_verified", "settled", "rejected", "cancelled",
  "delivery_failed", "returning", "returned", "exception"
];
