import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t as translate } from "@drlife/i18n";
import type { MerchantSuborder } from "./suborders";
import { SuborderDetailView, SuborderListView } from "./suborder-views";

const tr = (key: Parameters<typeof translate>[0]) => translate(key, undefined, "en");
const order: MerchantSuborder = {
  public_id: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  parent_public_id: "01ARZ3NDEKTSV4RRFFQ69G5FAW",
  status: "shipped",
    items: [{ sku_public_id: "SKU-2006", quantity: 8, unit_price_e4: 380000, line_total_e4: 3040000 }],
  shipping_address_public_id: "01ARZ3NDEKTSV4RRFFQ69G5FAX",
  shipping_address: { recipient_name: "Buyer F", address_text: "Phnom Penh", phone: "****1234" },
  goods_amount_e4: 3040000,
  shipping_amount_e4: 0,
  payable_amount_e4: 3040000,
  shipping_quote_expires_at: "2026-11-12T00:00:00Z",
  created_at: "2026-11-12T00:00:00Z",
  updated_at: "2026-11-12T00:00:00Z"
};

function text(markup: string): string[] {
  return markup.replace(/<[^>]*>/g, "\n").split("\n").map((item) => item.trim()).filter(Boolean);
}

describe("merchant sub-order views", () => {
  it("filters the interface-stub list by status", () => {
    const markup = renderToStaticMarkup(
      <SuborderListView language="en" translate={tr} orders={[order]} selected="shipped" onSelectStatus={() => undefined} onOpen={() => undefined} />
    );
    expect(text(markup)).toContain(order.public_id);
  });

  it("renders item, exact amount, and delivery status in detail", () => {
    const markup = renderToStaticMarkup(<SuborderDetailView order={order} language="en" translate={tr} onBack={() => undefined} />);
    expect(text(markup)).toContain(order.public_id);
    expect(text(markup)).toContain("$304.00");
    expect(text(markup).some((item) => item.includes("RouteFit shipped, in delivery"))).toBe(true);
  });
});
