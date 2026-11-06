import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t as translate } from "@drlife/i18n";
import { mockSuborders } from "./suborders";
import { SuborderDetailView, SuborderListView } from "./suborder-views";

const tr = (key: Parameters<typeof translate>[0]) => translate(key, undefined, "en");

function text(markup: string): string[] {
  return markup.replace(/<[^>]*>/g, "\n").split("\n").map((item) => item.trim()).filter(Boolean);
}

describe("merchant sub-order views", () => {
  it("filters the interface-stub list by status", () => {
    const markup = renderToStaticMarkup(
      <SuborderListView language="en" translate={tr} selected="shipped" onSelectStatus={() => undefined} onOpen={() => undefined} />
    );
    expect(text(markup)).toContain("SUB-2006");
    expect(text(markup)).not.toContain("SUB-2001");
  });

  it("renders item, exact amount, and delivery status in detail", () => {
    const order = mockSuborders[5]!;
    const markup = renderToStaticMarkup(<SuborderDetailView order={order} language="en" translate={tr} onBack={() => undefined} />);
    expect(text(markup)).toContain("SUB-2006");
    expect(text(markup)).toContain("$304.00");
    expect(text(markup).some((item) => item.includes("RouteFit shipped, in delivery"))).toBe(true);
  });
});
