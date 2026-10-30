import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { type Language, t as translate } from "@drlife/i18n";
import { ApiError } from "@drlife/api-client";
import { CategoryView } from "./views/category-view";
import { HomeView } from "./views/home-view";
import { ProductDetailView } from "./views/product-detail-view";
import { errorMessageKey } from "./resource";
import type { ProductFeed } from "./feed";
import type { Translate } from "./views/common";
import type { Category, Product } from "./types";

const languages: Language[] = ["zh", "en", "km"];

function translator(language: Language): Translate {
  return (key, params) => translate(key, params, language);
}

const categories: Category[] = [
  {
    public_id: "cat_tile",
    code: "c4",
    name: { zh: "瓷砖", en: "Tiles", km: "ក្បឿង" },
    enabled: true,
    sort: 10
  },
  {
    public_id: "cat_paint",
    code: "c8",
    name: { zh: "墙饰涂料", en: "Paint and wall", km: "ថ្នាំលាបជញ្ជាំង" },
    enabled: true,
    sort: 20
  }
];

const product: Product = {
  public_id: "prd_tile",
  category_code: "c4",
  name: {
    zh: "800×800 抛光砖",
    en: "Polished tile 800×800",
    km: "ក្បឿងប៉ូលា 800×800"
  },
  spec: {
    zh: "厚度 10mm，A 级",
    en: "10 mm thick, grade A",
    km: "កម្រាស់ 10mm ថ្នាក់ A"
  },
  unit: "m2",
  origin: "cn",
  lead_days: 15,
  status: "on",
  source: "manual",
  tiers: [
    { min_qty: "500", unit_price_e4: 82_000 },
    { min_qty: "1", unit_price_e4: 95_000 },
    { min_qty: "100", unit_price_e4: 88_000 }
  ],
  updated_at: "2026-10-29T00:00:00Z"
};

const localProduct: Product = {
  ...product,
  public_id: "prd_paint",
  category_code: "c8",
  name: { zh: "乳胶漆 18L", en: "Latex paint 18 L", km: "ថ្នាំលាប 18L" },
  spec: undefined,
  unit: "pail",
  origin: "lc",
  lead_days: null,
  tiers: [{ min_qty: "1", unit_price_e4: 380_000 }]
};

const noOp = () => undefined;

function home(
  language: Language,
  overrides: Partial<Parameters<typeof HomeView>[0]> = {}
) {
  return renderToStaticMarkup(
    <HomeView
      t={translator(language)}
      language={language}
      categories={{ status: "ready", value: categories }}
      featured={{ status: "ready", value: [product, localProduct] }}
      onRetryCategories={noOp}
      onRetryFeatured={noOp}
      onSelectCategory={noOp}
      onSelectProduct={noOp}
      {...overrides}
    />
  );
}

function feed(overrides: Partial<ProductFeed> = {}): ProductFeed {
  return { items: [product], page: 1, total: 1, hasMore: false, ...overrides };
}

function category(
  language: Language,
  overrides: Partial<Parameters<typeof CategoryView>[0]> = {}
) {
  return renderToStaticMarkup(
    <CategoryView
      t={translator(language)}
      language={language}
      categories={categories}
      activeCode="c4"
      products={{ status: "ready", value: feed() }}
      loadingMore={false}
      onRetry={noOp}
      onLoadMore={noOp}
      onBack={noOp}
      onSelectCategory={noOp}
      onSelectProduct={noOp}
      {...overrides}
    />
  );
}

function detail(
  language: Language,
  overrides: Partial<Parameters<typeof ProductDetailView>[0]> = {}
) {
  return renderToStaticMarkup(
    <ProductDetailView
      t={translator(language)}
      language={language}
      product={product}
      onBack={noOp}
      {...overrides}
    />
  );
}

const entities: Record<string, string> = {
  "&amp;": "&",
  "&lt;": "<",
  "&gt;": ">",
  "&quot;": '"',
  "&#x27;": "'",
  "&#39;": "'"
};

/** The toolchain has no DOM, so assertions read the server-rendered markup. */
function visibleText(markup: string): string[] {
  return markup
    .replace(/<[^>]*>/g, "\n")
    .replace(/&[a-z]+;|&#x?[0-9a-f]+;/gi, (entity) => entities[entity] ?? entity)
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

describe("home view", () => {
  it("shows the search entry, the category grid and the recommended products", () => {
    const text = visibleText(home("en"));
    expect(text).toContain("Search products");
    expect(text).toContain("Categories");
    expect(text).toContain("Tiles");
    expect(text).toContain("Featured materials");
    expect(text).toContain("Polished tile 800×800");
  });

  it("prices a product from its lowest minimum quantity tier", () => {
    expect(visibleText(home("en"))).toContain("from $9.50");
  });

  it("reports loading for each section", () => {
    const markup = home("en", {
      categories: { status: "loading" },
      featured: { status: "loading" }
    });
    expect(markup.match(/role="status"/g)).toHaveLength(2);
    expect(visibleText(markup).filter((line) => line === "Loading")).toHaveLength(2);
  });

  it("reports an empty catalogue with a way to reload", () => {
    const text = visibleText(
      home("en", {
        categories: { status: "ready", value: [] },
        featured: { status: "ready", value: [] }
      })
    );
    expect(text).toContain("No categories yet");
    expect(text).toContain("No products on sale");
    expect(text.filter((line) => line === "Refresh")).toHaveLength(2);
  });

  it("reports a failed load with the translated server message and a retry", () => {
    const markup = home("en", {
      categories: {
        status: "error",
        messageKey: errorMessageKey(new ApiError(1005, "req_1", 503))
      },
      featured: { status: "ready", value: [product] }
    });
    const text = visibleText(markup);
    expect(markup).toContain('role="alert"');
    expect(text).toContain("Could not load");
    expect(text).toContain("The service is temporarily unavailable. Try again later.");
    expect(text).toContain("Retry");
  });
});

describe("category view", () => {
  it("lists the categories beside the products of the active one", () => {
    const markup = category("en");
    const text = visibleText(markup);
    expect(text).toContain("All");
    expect(text).toContain("Tiles");
    expect(text).toContain("1 product");
    expect(text).toContain("Polished tile 800×800");
    expect(markup).toContain('aria-pressed="true"');
  });

  it("covers loading, empty and failed product lists", () => {
    expect(category("en", { products: { status: "loading" } })).toContain(
      'role="status"'
    );
    expect(
      visibleText(
        category("en", {
          products: { status: "ready", value: feed({ items: [], total: 0 }) }
        })
      )
    ).toContain("No products on sale");
    const failed = category("en", {
      products: { status: "error", messageKey: "common.networkError" }
    });
    expect(failed).toContain('role="alert"');
    expect(visibleText(failed)).toContain("Network error");
  });

  /** The server returns one page, so the page length is not the category size. */
  it("counts the products the server reported, not the rows on this page", () => {
    const text = visibleText(
      category("en", {
        products: {
          status: "ready",
          value: feed({ items: [product], total: 57, hasMore: true })
        }
      })
    );

    expect(text).toContain("57 products");
    expect(text).not.toContain("1 product");
  });

  it("offers a way to reach the products past the first page", () => {
    const more = category("en", {
      products: {
        status: "ready",
        value: feed({ items: [product], total: 57, hasMore: true })
      }
    });
    expect(visibleText(more)).toContain("Show more");

    expect(visibleText(category("en"))).not.toContain("Show more");
  });

  it("marks the load-more control busy while the next page is in flight", () => {
    const markup = category("en", {
      products: {
        status: "ready",
        value: feed({ items: [product], total: 57, hasMore: true })
      },
      loadingMore: true
    });

    expect(markup).toContain('aria-busy="true"');
    expect(markup).toContain("disabled");
    expect(visibleText(markup)).toContain("Loading");
  });

  it("keeps the loaded rows and reports a failed next page", () => {
    const markup = category("en", {
      products: {
        status: "ready",
        value: feed({ items: [product], total: 57, hasMore: true })
      },
      moreMessageKey: "common.networkError"
    });
    const text = visibleText(markup);

    expect(markup).toContain('role="alert"');
    expect(text).toContain("Network error");
    expect(text).toContain("Polished tile 800×800");
    expect(text).toContain("Show more");
  });

  it("shows no count at all for an empty category", () => {
    const text = visibleText(
      category("en", {
        products: { status: "ready", value: feed({ items: [], total: 0 }) }
      })
    );

    expect(text).not.toContain("0 products");
    expect(text).toContain("No products on sale");
  });
});

describe("product detail view", () => {
  it("shows the trilingual name, spec, unit, tier prices and lead time", () => {
    const text = visibleText(detail("en"));
    expect(text).toContain("Polished tile 800×800");
    expect(text).toContain("10 mm thick, grade A");
    expect(text).toContain("From China");
    expect(text).toContain("Tier pricing (more = cheaper)");
    expect(text).toContain("1 and up");
    expect(text).toContain("$9.50");
    expect(text).toContain("100 and up");
    expect(text).toContain("$8.80");
    expect(text).toContain("500 and up");
    expect(text).toContain("$8.20");
    expect(text).toContain("MOQ");
    expect(text).toContain("Unit");
    expect(text).toContain("m2");
    expect(text).toContain("15 days");
  });

  it("orders the tiers by minimum quantity", () => {
    const text = visibleText(detail("en"));
    expect(text.indexOf("1 and up")).toBeLessThan(text.indexOf("100 and up"));
    expect(text.indexOf("100 and up")).toBeLessThan(text.indexOf("500 and up"));
  });

  it("hides the supplier row until the API returns a merchant", () => {
    expect(visibleText(detail("en"))).not.toContain("Supplier");
    const named = visibleText(detail("en", { merchantName: "Dr Building Supply" }));
    expect(named).toContain("Supplier");
    expect(named).toContain("Dr Building Supply");
  });

  it("omits the lead time row when the product has none", () => {
    expect(visibleText(detail("en", { product: localProduct }))).not.toContain(
      "Lead time"
    );
  });
});

describe("khmer rendering", () => {
  it("marks catalogue text with its language so the Khmer font applies", () => {
    expect(home("km")).toContain('lang="km"');
    expect(category("km")).toContain('lang="km"');
    expect(detail("km")).toContain('lang="km"');
  });

  /**
   * The rules that stop Khmer from being clipped live in styles.css and are
   * asserted in styles.test.ts. This guards the other half: no view may
   * override them with an inline style.
   */
  it("renders no inline style, so the stylesheet keeps the last word", () => {
    for (const markup of [home("km"), category("km"), detail("km")]) {
      expect(markup).not.toMatch(/style="/);
      expect(markup).not.toMatch(/text-overflow|nowrap|line-clamp/);
    }
  });
});

describe("trilingual snapshots", () => {
  for (const language of languages) {
    it(`renders the home view in ${language}`, () => {
      expect(visibleText(home(language))).toMatchSnapshot();
    });

    it(`renders the category view in ${language}`, () => {
      expect(visibleText(category(language))).toMatchSnapshot();
    });

    it(`renders the product detail view in ${language}`, () => {
      expect(
        visibleText(detail(language, { merchantName: "Dr Building Supply" }))
      ).toMatchSnapshot();
    });
  }
});
