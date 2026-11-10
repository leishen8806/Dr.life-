// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type Language, t as translate } from "@drlife/i18n";
import type { BuyerOrder } from "./orders";
import { OrderDetailView } from "./views/order-detail-view";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function order(status: BuyerOrder["subOrders"][number]["status"], expiresAt: string): BuyerOrder {
  return {
    publicId: "ord_test",
    createdAt: "2026-11-11T00:00:00Z",
    totalE4: 10_000n,
    goodsAmountE4: 10_000n,
    shippingAmountE4: 0n,
    paymentStatus: "pending",
    subOrders: [{
      publicId: "sub_test",
      merchantName: { zh: "商家", en: "Merchant", km: "ហាង" },
      status,
      lines: [],
      goodsAmountE4: 10_000n,
      totalE4: 10_000n,
      shippingAmountE4: 0n,
      shippingQuoteExpiresAt: expiresAt,
      events: []
    }]
  };
}

function renderDetail(value: BuyerOrder, onPay?: () => void) {
  const language: Language = "en";
  return render(
    <OrderDetailView
      t={(key, params) => translate(key, params, language)}
      language={language}
      order={{ status: "ready", value }}
      onBack={() => undefined}
      onPay={onPay}
    />
  );
}

describe("OrderDetailView quote expiry", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-11T00:00:00Z"));
  });

  it("switches from valid to expired at the quote deadline", () => {
    renderDetail(order("awaiting_payment", "2026-11-11T00:00:01Z"));
    expect(screen.getAllByText("Order details").length).toBeGreaterThan(0);
    expect(screen.getByText("Sub-order")).toBeTruthy();
    expect(screen.getAllByText("Free").length).toBeGreaterThan(0);
    expect(screen.queryByText("ord_test")).toBeNull();
    expect(screen.queryByText("sub_test")).toBeNull();
    expect(screen.getByText(/Shipping quote expires/)).toBeTruthy();

    vi.setSystemTime(new Date("2026-11-11T00:00:02Z"));
    act(() => vi.runOnlyPendingTimers());

    expect(screen.getByText(/Shipping quote expired/)).toBeTruthy();
  });

  it("does not show a quote for a non-payment order", () => {
    renderDetail(order("paid", "2026-11-11T00:00:01Z"));
    expect(screen.queryByText(/Shipping quote/)).toBeNull();
  });

  it("does not offer payment while a quote is expired or pending", () => {
    const onPay = vi.fn();
    renderDetail(order("awaiting_payment", "2026-11-10T23:59:00Z"), onPay);
    expect(screen.queryByText("Pay now")).toBeNull();

    cleanup();
    renderDetail(order("awaiting_payment", ""), onPay);
    expect(screen.getByText("Quote pending")).toBeTruthy();
    expect(screen.queryByText("Pay now")).toBeNull();
  });
});
