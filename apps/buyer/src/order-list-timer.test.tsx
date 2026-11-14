// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t as translate } from "@drlife/i18n";
import type { BuyerOrder } from "./orders";
import { OrderListView } from "./views/order-list-view";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function order(expiresAt: string): BuyerOrder {
  return {
    publicId: "ord_timer",
    createdAt: "2026-11-14T00:00:00Z",
    totalE4: 10_000n,
    goodsAmountE4: 10_000n,
    shippingAmountE4: 0n,
    paymentStatus: "pending",
    subOrders: [{
      publicId: "sub_timer",
      merchantName: { zh: "商家", en: "Merchant", km: "ហាង" },
      status: "awaiting_payment",
      lines: [],
      goodsAmountE4: 10_000n,
      totalE4: 10_000n,
      shippingAmountE4: 0n,
      shippingQuoteExpiresAt: expiresAt,
      events: []
    }]
  };
}

function view(value: BuyerOrder) {
  return (
    <OrderListView
      t={(key, params) => translate(key, params, "en")}
      language="en"
      orders={{ status: "ready", value: [value] }}
      onBack={() => undefined}
      onRetry={() => undefined}
      onOpen={() => undefined}
    />
  );
}

describe("OrderListView quote timer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-14T00:00:00Z"));
  });

  it("does not rebuild the timer when the same orders resource rerenders", () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    const value = order("2026-11-14T00:00:10Z");
    const { rerender } = render(view(value));

    expect(setTimeoutSpy).toHaveBeenCalledTimes(1);
    rerender(view(value));
    expect(setTimeoutSpy).toHaveBeenCalledTimes(1);
  });

  it("rebuilds the timer only when the nearest quote expiry changes", () => {
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    const first = order("2026-11-14T00:00:10Z");
    const { rerender } = render(view(first));
    expect(setTimeoutSpy).toHaveBeenCalledTimes(1);

    rerender(view(order("2026-11-14T00:00:20Z")));
    expect(setTimeoutSpy).toHaveBeenCalledTimes(2);

    act(() => vi.runOnlyPendingTimers());
    expect(setTimeoutSpy).toHaveBeenCalledTimes(2);
  });
});
