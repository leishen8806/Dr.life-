import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { type Language, t as translate } from "@drlife/i18n";
import { mockOrders } from "./orders";
import { OrderDetailView } from "./views/order-detail-view";
import { OrderListView } from "./views/order-list-view";
import type { Translate } from "./views/common";

function translator(language: Language): Translate {
  return (key, params) => translate(key, params, language);
}

function text(markup: string): string[] {
  return markup
    .replace(/<[^>]*>/g, "\n")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .split("\n")
    .map((item) => item.trim())
    .filter(Boolean);
}

describe("buyer order views", () => {
  it("covers parent orders, split merchants and payment confirmation", () => {
    const markup = renderToStaticMarkup(
      <OrderListView
        t={translator("en")}
        language="en"
        orders={{ status: "ready", value: mockOrders }}
        onBack={() => undefined}
        onRetry={() => undefined}
        onOpen={() => undefined}
        onLoadMore={() => undefined}
      />
    );
    expect(mockOrders).toHaveLength(17);
    expect(text(markup)).toContain("Awaiting payment confirmation");
    expect(text(markup)).toContain("Warehouse processing, awaiting acceptance");
    expect(text(markup)).toContain("$114.00");
    expect(text(markup).some((item) => item.includes("11/1/26"))).toBe(true);
  });

  it("renders loading, empty and error resources", () => {
    const props = {
      t: translator("en"),
      language: "en" as const,
      onBack: () => undefined,
      onRetry: () => undefined,
      onOpen: () => undefined
    };
    expect(text(renderToStaticMarkup(<OrderListView {...props} orders={{ status: "loading" }} />))).toContain("Loading");
    expect(text(renderToStaticMarkup(<OrderListView {...props} orders={{ status: "ready", value: [] }} />))).toContain("No orders yet");
    expect(text(renderToStaticMarkup(<OrderListView {...props} orders={{ status: "error", messageKey: "common.networkError" }} />))).toContain("Network error");
  });

  it("renders a localized detail with exact bigint-derived amounts and delivery state", () => {
    const order = mockOrders[2]!;
    const markup = renderToStaticMarkup(
      <OrderDetailView
        t={translator("zh")}
        language="zh"
        order={{ status: "ready", value: order }}
        onBack={() => undefined}
      />
    );
    expect(text(markup)).toContain("订单详情");
    expect(text(markup)).toContain("仓库处理中，等待受理");
    expect(text(markup)).toContain("$880.00");
    expect(text(markup)).toContain("配送进度");
  });

  it("shows the confirming payment guard without a payment action", () => {
    const markup = renderToStaticMarkup(
      <OrderDetailView
        t={translator("en")}
        language="en"
        order={{ status: "ready", value: mockOrders[10]! }}
        onBack={() => undefined}
      />
    );
    expect(text(markup)).toContain("Payment result is being confirmed. Please do not pay again.");
    expect(text(markup)).not.toContain("Pay now");
  });

  it("does not show an empty delivery section", () => {
    const order = { ...mockOrders[2]!, subOrders: mockOrders[2]!.subOrders.map((subOrder) => ({ ...subOrder, events: [] })) };
    const markup = renderToStaticMarkup(
      <OrderDetailView
        t={translator("en")}
        language="en"
        order={{ status: "ready", value: order }}
        onBack={() => undefined}
      />
    );
    expect(text(markup)).not.toContain("Delivery progress");
  });
});
