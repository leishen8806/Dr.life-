import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { type Language, t as translate } from "@drlife/i18n";
import { mockOrders } from "./orders";
import { PaymentView } from "./views/payment-view";
import type { Translate } from "./views/common";

function translator(language: Language): Translate {
  return (key, params) => translate(key, params, language);
}

const noop = () => undefined;

function payment(status: "pending" | "confirming" | "succeeded" | "failed" | "expired") {
  return renderToStaticMarkup(
    <PaymentView
      t={translator("en")}
      language="en"
      order={mockOrders[0]!}
      attempt={{ status, idempotencyKey: "idem-ORD-1001", paymentNumber: "PAY-ORD-1001" }}
      onBack={noop}
      onPay={noop}
      onRetry={noop}
      onRefresh={noop}
    />
  );
}

describe("payment confirmation view", () => {
  it("shows pending payment and shipping details", () => {
    const markup = payment("pending");
    expect(markup).toContain("Pay now");
    expect(markup).toContain("Free shipping");
    expect(markup).toContain("Shipping quote valid until 2026-11-01");
  });

  it("hides payment actions while confirming", () => {
    const markup = payment("confirming");
    expect(markup).toContain("Payment result is being confirmed. Please do not pay again.");
    expect(markup).not.toContain("Pay now");
    expect(markup).not.toContain("Pay again");
  });

  it("covers succeeded and failed/expired outcomes", () => {
    expect(payment("succeeded")).toContain("Paid, warehouse processing");
    expect(payment("failed")).toContain("Payment was not completed.");
    expect(payment("expired")).toContain("Pay again");
  });
});
