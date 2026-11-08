import type { BuyerOrder } from "./orders";

export type PaymentFlowStatus = "pending" | "confirming" | "succeeded" | "failed";

export interface PaymentAttempt {
  paymentNumber?: string;
  paymentLink?: string;
  idempotencyKey: string;
  status: PaymentFlowStatus;
  expiresAt?: string;
}

export function createPaymentIdempotencyKey(): string {
  const randomUUID = globalThis.crypto?.randomUUID;
  if (randomUUID) return randomUUID.call(globalThis.crypto);
  return `payment-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export function pendingPaymentAttempt(_order: BuyerOrder, idempotencyKey: string): PaymentAttempt {
  return { idempotencyKey, status: "pending" };
}

export function createMockPaymentAttempt(
  order: BuyerOrder,
  idempotencyKey: string,
  attemptNumber: number
): PaymentAttempt {
  return {
    paymentNumber: `PAY-${order.publicId}-${attemptNumber}`,
    paymentLink: `https://t.me/dr-life-demo-payment?order=${encodeURIComponent(order.publicId)}&payment=${attemptNumber}`,
    idempotencyKey,
    status: "confirming"
  };
}

export function canPay(order: BuyerOrder): boolean {
  return order.paymentStatus === "pending" && order.subOrders.every((item) => item.status === "awaiting_payment");
}
