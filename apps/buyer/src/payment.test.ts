import { describe, expect, it } from "vitest";
import { createMockPaymentAttempt, canPay, createPaymentIdempotencyKey, pendingPaymentAttempt } from "./payment";
import { mockOrders, payableAmountE4 } from "./orders";

describe("mock payment flow", () => {
  it("only exposes payment for a pending parent with awaiting-payment children", () => {
    expect(canPay(mockOrders[0]!)).toBe(true);
    expect(canPay(mockOrders[10]!)).toBe(false);
    expect(canPay({ ...mockOrders[0]!, paymentStatus: "succeeded" })).toBe(false);
  });

  it("reuses the same idempotency key for payment retries", () => {
    const order = mockOrders[0]!;
    const first = createMockPaymentAttempt(order, "idem-ORD-1001", 1);
    const retry = createMockPaymentAttempt(order, first.idempotencyKey, 2);
    expect(retry.idempotencyKey).toBe(first.idempotencyKey);
    expect(retry.paymentNumber).not.toBe(first.paymentNumber);
    expect(retry.paymentLink).not.toBe(first.paymentLink);
  });

  it("keeps pending attempts free of a payment link until the click", () => {
    const pending = pendingPaymentAttempt(mockOrders[0]!, "idem-ORD-1001");
    expect(pending.status).toBe("pending");
    expect(pending.paymentLink).toBeUndefined();
    expect(pending.paymentNumber).toBeUndefined();
  });

  it("creates a fresh idempotency key for each payment attempt", () => {
    expect(createPaymentIdempotencyKey()).not.toBe(createPaymentIdempotencyKey());
  });

  it("adds shipping to the payable amount without floating point math", () => {
    const failed = mockOrders[16]!;
    expect(payableAmountE4(failed.subOrders[0]!)).toBe(297_500n);
  });
});
