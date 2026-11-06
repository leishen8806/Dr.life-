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

describe("buyer order mock flow", () => {
  it("covers every order status in the list and keeps the interface-stub label", () => {
    const markup = renderToStaticMarkup(
      <OrderListView
        t={translator("en")}
        language="en"
        orders={{ status: "ready", value: mockOrders }}
        onBack={() => undefined}
        onRetry={() => undefined}
        onOpen={() => undefined}
      />
    );
    expect(mockOrders).toHaveLength(10);
    expect(text(markup)).toContain("Interface-stub data for the buyer order flow");
    expect(text(markup)).toContain("Pending payment");
    expect(text(markup)).toContain("Rejected");
    expect(text(markup)).toContain("$114.00");
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
        order={order}
        onBack={() => undefined}
      />
    );
    expect(text(markup)).toContain("订单详情");
    expect(text(markup)).toContain("已提交");
    expect(text(markup)).toContain("$880.00");
    expect(text(markup)).toContain("配送进度");
  });
});
