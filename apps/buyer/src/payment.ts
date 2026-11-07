import type { BuyerOrder } from "./orders";

export type PaymentFlowStatus = "pending" | "confirming" | "succeeded" | "failed" | "expired";

export interface PaymentAttempt {
  paymentNumber?: string;
  paymentLink?: string;
  idempotencyKey: string;
  status: PaymentFlowStatus;
}

export function pendingPaymentAttempt(order: BuyerOrder, idempotencyKey: string): PaymentAttempt {
  return { idempotencyKey, status: "pending", paymentNumber: `PAY-${order.publicId}` };
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
