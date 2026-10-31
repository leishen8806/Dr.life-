// @vitest-environment jsdom
/**
 * 视图层的断言在 product-views.test.tsx，继续跑在默认的 node 环境下。
 * 这里只验证需要 DOM 才能验证的事：屏幕装载后把接口返回的商品显示出来，
 * 以及"新建"把列表换成表单。
 */
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { I18nProvider, t } from "@drlife/i18n";
import type { MerchantProductApi } from "./api";
import type { Category, Product } from "./form";
import { ProductScreen } from "./product-screen";

afterEach(cleanup);

const product: Product = {
  public_id: "01HZY0000000000000000MERCH",
  category_code: "rice",
  name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
  unit: "bag",
  origin: "cn",
  status: "on",
  source: "manual",
  tiers: [{ min_qty: "10", unit_price_e4: 125_000 }],
  skus: [],
  updated_at: "2026-10-29T00:00:00Z"
};

const categories: Category[] = [
  {
    public_id: "01HZY0000000000000000CATEG",
    code: "rice",
    name: { zh: "米", en: "Rice", km: "អង្ករ" },
    enabled: true,
    sort: 1
  }
];

/** 只有读接口会被用到；写接口被调到就是测试本身写错了。 */
function readOnlyApi(): MerchantProductApi {
  const unexpected = () => Promise.reject(new Error("no write in this test"));
  return {
    list: () => Promise.resolve([product]),
    listCategories: () => Promise.resolve(categories),
    create: unexpected,
    update: unexpected,
    publish: unexpected
  };
}

describe("ProductScreen", () => {
  it("shows the loaded products and opens the form on create", async () => {
    render(
      <I18nProvider initialLanguage="en">
        <ProductScreen api={readOnlyApi()} />
      </I18nProvider>
    );

    expect(await screen.findByText("Jasmine rice 5kg")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: t("product.list.create") }));

    expect(screen.queryByText("Jasmine rice 5kg")).toBeNull();
  });
});
