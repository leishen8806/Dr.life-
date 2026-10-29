import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { TranslationKey } from "@drlife/i18n";
import { ProductFormView, ProductListView } from "./product-views";
import { emptyProductDraft, type Category, type Product } from "./form";

/** 文案全部回显词条名，断言就能证明界面没有硬编码的字面量。 */
const translate = (key: TranslationKey, params: Record<string, string | number> = {}) =>
  Object.keys(params).length ? `${key}(${Object.values(params).join("|")})` : key;

function product(status: Product["status"], publicId = "01HZY0000000000000000MERCH"): Product {
  return {
    public_id: publicId,
    category_code: "rice",
    name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
    unit: "bag",
    origin: "cn",
    status,
    source: "manual",
    tiers: [
      { min_qty: "100", unit_price_e4: 118000 },
      { min_qty: "10", unit_price_e4: 125000 }
    ],
    skus: [],
    updated_at: "2026-10-29T00:00:00Z"
  };
}

const categories: Category[] = [
  {
    public_id: "01HZY0000000000000000CATEG",
    code: "rice",
    name: { zh: "米", en: "Rice", km: "អង្ករ" },
    enabled: true,
    sort: 1
  }
];

function listMarkup(products: Product[], language: "zh" | "en" | "km" = "en") {
  return renderToStaticMarkup(
    <ProductListView
      products={products}
      language={language}
      translate={translate}
      onCreate={vi.fn()}
      onEdit={vi.fn()}
      onPublish={vi.fn()}
      onUnpublish={vi.fn()}
    />
  );
}

describe("ProductListView", () => {
  it("offers creating a product when the merchant has none", () => {
    const markup = listMarkup([]);

    expect(markup).toContain("product.list.empty");
    expect(markup).toContain("product.list.create");
  });

  it("shows the name in the active language and the tier the buyer starts at", () => {
    const markup = listMarkup([product("on")], "km");

    expect(markup).toContain("អង្ករម្លិះ 5kg");
    // 起步档是起订量最小的那一档，不是接口返回的第一条。
    expect(markup).toContain("product.tier.summary(12.5|10|bag)");
  });

  it("tags every status with its own tone", () => {
    expect(listMarkup([product("draft", "01HZY000000000000000000001")])).toContain(
      "drl-status-tag--neutral"
    );
    expect(listMarkup([product("review", "01HZY000000000000000000002")])).toContain(
      "drl-status-tag--warning"
    );
    expect(listMarkup([product("on", "01HZY000000000000000000003")])).toContain(
      "drl-status-tag--success"
    );
    expect(listMarkup([product("rejected", "01HZY000000000000000000004")])).toContain(
      "drl-status-tag--danger"
    );
  });

  it("only offers the actions the status allows", () => {
    const draft = listMarkup([product("draft")]);
    expect(draft).toContain("product.action.publish");
    expect(draft).not.toContain("product.action.unpublish");

    const onSale = listMarkup([product("on")]);
    expect(onSale).toContain("product.action.unpublish");
    expect(onSale).not.toContain("product.action.publish");

    const review = listMarkup([product("review")]);
    expect(review).not.toContain("product.action.edit");
    expect(review).not.toContain("product.action.publish");
  });
});

describe("ProductFormView", () => {
  function formMarkup(overrides: { errors?: { field: string; messageKey: TranslationKey }[] } = {}) {
    return renderToStaticMarkup(
      <ProductFormView
        mode="create"
        draft={emptyProductDraft()}
        errors={overrides.errors ?? []}
        categories={categories}
        language="en"
        translate={translate}
        busy={false}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />
    );
  }

  it("asks for the name and the spec in all three languages", () => {
    const markup = formMarkup();

    for (const language of ["language.zh", "language.en", "language.km"]) {
      expect(markup).toContain(`product.field.name(${language})`);
      expect(markup).toContain(`product.field.spec(${language})`);
    }
  });

  it("lets Khmer wrap instead of truncating it", () => {
    expect(formMarkup()).toContain('lang="km" class="drl-khmer"');
  });

  it("starts on the two tiers that listing requires", () => {
    const markup = formMarkup();

    expect(markup).toContain("product.field.minQty");
    expect(markup.match(/product\.field\.unitPrice/g)).toHaveLength(2);
    expect(markup).toContain("product.tier.add");
  });

  it("puts each field error next to its own field", () => {
    const markup = formMarkup({
      errors: [
        { field: "name.km", messageKey: "product.error.nameTrilingual" },
        { field: "tiers[1].min_qty", messageKey: "product.error.minQtyDuplicate" }
      ]
    });

    expect(markup).toContain("product.error.nameTrilingual");
    expect(markup).toContain("product.error.minQtyDuplicate");
    expect(markup).not.toContain("product.error.unitRequired");
  });

  it("lists the categories the platform enabled", () => {
    expect(formMarkup()).toContain('value="rice"');
  });
});
