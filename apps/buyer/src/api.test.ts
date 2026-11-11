import { afterEach, describe, expect, it, vi } from "vitest";
import { readLanguagePreference, storeLanguagePreference } from "@drlife/i18n";
import { ApiError } from "@drlife/api-client";
import { createTelegramSession } from "@drlife/api-client/session";
import { createBuyerApiClient, loadCategories, loadProducts, loadBuyerOrders, loadBuyerOrder, searchProducts, suggestProducts } from "./api";
import type { Category, Product } from "./types";

afterEach(() => {
  vi.restoreAllMocks();
});

const category: Category = {
  public_id: "cat_tile",
  code: "c4",
  name: { zh: "瓷砖", en: "Tiles", km: "ក្បឿង" },
  enabled: true,
  sort: 10
};

const product: Product = {
  public_id: "prd_tile",
  category_code: "c4",
  name: { zh: "800×800 抛光砖", en: "Polished tile 800×800", km: "ក្បឿង 800×800" },
  spec: { zh: "厚度 10mm，A 级", en: "10 mm thick, grade A", km: "កម្រាស់ 10mm" },
  unit: "m2",
  origin: "cn",
  lead_days: 15,
  status: "on",
  source: "manual",
  tiers: [
    { min_qty: 1, unit_price_e4: 95_000 },
    { min_qty: 100, unit_price_e4: 88_000 }
  ],
  skus: [],
  updated_at: "2026-10-29T00:00:00Z"
};

function recordingFetch(response: (call: number) => Response) {
  const urls: string[] = [];
  const transport: typeof globalThis.fetch = async (input, init) => {
    urls.push(new Request(input, init).url);
    return response(urls.length - 1);
  };
  return { urls, transport };
}

function success(data: unknown): Response {
  return new Response(JSON.stringify({ ok: true, data, request_id: "req_test" }), {
    status: 200,
    headers: { "content-type": "application/json" }
  });
}

function failure(code: number, status: number): Response {
  return new Response(
    JSON.stringify({
      ok: false,
      error: { code, message: { zh: "错误", en: "Error", km: "កំហុស" } },
      request_id: "req_test"
    }),
    { status, headers: { "content-type": "application/json" } }
  );
}

function client(transport: typeof globalThis.fetch) {
  return createBuyerApiClient({
    baseUrl: "http://api.test",
    getLanguage: () => "km",
    fetch: transport,
    resilience: { retry: { maxAttempts: 1 } }
  });
}

describe("loadCategories", () => {
  it("asks for every enabled category and keeps the server order", async () => {
    const second: Category = { ...category, public_id: "cat_paint", code: "c8", sort: 20 };
    const { urls, transport } = recordingFetch(() =>
      success({ items: [category, second], page: 1, page_size: 100, total: 2 })
    );

    await expect(loadCategories(client(transport))).resolves.toEqual([
      category,
      second
    ]);
    expect(urls[0]).toBe("http://api.test/v1/public/categories?page_size=100");
  });

  /** One page holds 100 categories; a longer catalogue must not be cut off. */
  it("follows the remaining pages when the total exceeds one page", async () => {
    const first = Array.from({ length: 100 }, (_, index) => ({
      ...category,
      public_id: `cat_${index}`,
      code: `c${index}`
    }));
    const last: Category = { ...category, public_id: "cat_last", code: "c100" };
    const { urls, transport } = recordingFetch((call) =>
      call === 0
        ? success({ items: first, page: 1, page_size: 100, total: 101 })
        : success({ items: [last], page: 2, page_size: 100, total: 101 })
    );

    const categories = await loadCategories(client(transport));

    expect(categories).toHaveLength(101);
    expect(categories[100]).toEqual(last);
    expect(urls).toEqual([
      "http://api.test/v1/public/categories?page_size=100",
      "http://api.test/v1/public/categories?page=2&page_size=100"
    ]);
  });
});

describe("loadProducts", () => {
  it("filters by category code and page size", async () => {
    const { urls, transport } = recordingFetch(() =>
      success({ items: [product], page: 1, page_size: 20, total: 1 })
    );

    await expect(
      loadProducts(client(transport), { category: "c4", pageSize: 20 })
    ).resolves.toEqual({ items: [product], page: 1, page_size: 20, total: 1 });
    expect(urls[0]).toBe(
      "http://api.test/v1/public/products?page_size=20&category=c4"
    );
  });

  /** The count label reads `total`, so the envelope must survive the call. */
  it("keeps the page numbers and the total the server reported", async () => {
    const { transport } = recordingFetch(() =>
      success({ items: [product], page: 1, page_size: 20, total: 57 })
    );

    await expect(loadProducts(client(transport), { category: "c4" })).resolves
      .toMatchObject({ page: 1, page_size: 20, total: 57 });
  });

  it("asks for the page the feed is on", async () => {
    const { urls, transport } = recordingFetch(() =>
      success({ items: [product], page: 3, page_size: 20, total: 57 })
    );

    await loadProducts(client(transport), { category: "c4", page: 3 });

    expect(urls[0]).toBe(
      "http://api.test/v1/public/products?page=3&page_size=20&category=c4"
    );
  });

  it("omits the category filter on the recommended list", async () => {
    const { urls, transport } = recordingFetch(() =>
      success({ items: [], page: 1, page_size: 6, total: 0 })
    );

    await expect(
      loadProducts(client(transport), { pageSize: 6 })
    ).resolves.toEqual({ items: [], page: 1, page_size: 6, total: 0 });
    expect(urls[0]).toBe("http://api.test/v1/public/products?page_size=6");
  });

  it("rejects with the server error code so the view can translate it", async () => {
    const { transport } = recordingFetch(() => failure(1002, 400));

    await expect(loadProducts(client(transport), {})).rejects.toMatchObject({
      name: "ApiError",
      code: 1002,
      messageKey: "error.1002"
    });
  });

  it("propagates a transport failure", async () => {
    const transport: typeof globalThis.fetch = async () => {
      throw new TypeError("fetch failed");
    };

    await expect(loadProducts(client(transport), {})).rejects.toBeInstanceOf(
      TypeError
    );
  });

  it("does not swallow an error envelope into an empty list", async () => {
    const { transport } = recordingFetch(() => failure(1005, 503));

    await expect(loadProducts(client(transport), {})).rejects.toBeInstanceOf(
      ApiError
    );
  });
});

describe("searchProducts", () => {
  it("passes the generated search contract without hand-built URLs", async () => {
    const { urls, transport } = recordingFetch(() => success({ items: [product], next_cursor: "next", recommendations: [], reason: null }));
    await expect(searchProducts(client(transport), {
      q: "tile",
      category: "c4",
      minPriceE4: 12500,
      maxPriceE4: 25000,
      sort: "price_asc",
      cursor: "cursor-1",
      pageSize: 20
    })).resolves.toMatchObject({ items: [product], next_cursor: "next" });
    expect(urls[0]).toBe("http://api.test/v1/public/search/products?q=tile&category=c4&min_price_e4=12500&max_price_e4=25000&sort=price_asc&cursor=cursor-1&page_size=20");
  });

  it("loads server-provided suggestions", async () => {
    const { urls, transport } = recordingFetch(() => success({ items: [{ text: "tile" }] }));
    await expect(suggestProducts(client(transport), "ti")).resolves.toEqual(["tile"]);
    expect(urls[0]).toBe("http://api.test/v1/public/search/suggest?q=ti");
  });
});

const apiOrder = {
  public_id: "01JORDER",
  shipping_address_public_id: "01JADDRESS",
  shipping_address: { recipient_name: "Buyer", address_text: "Phnom Penh", phone: "+85512345678" },
  payment_status: "pending" as const,
  summary_status: "awaiting_payment" as const,
  suborders: [{
    public_id: "01JSUBORDER",
    merchant_public_id: "01JMERCHANT",
    status: "awaiting_payment" as const,
    items: [{ sku_public_id: "01JSKU", quantity: 2, unit_price_e4: 95_000, line_total_e4: 190_000 }],
    goods_amount_e4: 190_000,
    shipping_amount_e4: 0,
    payable_amount_e4: 190_000,
    shipping_quote_expires_at: "2026-11-08T10:00:00Z"
  }],
  goods_amount_e4: 190_000,
  shipping_amount_e4: 0,
  payable_amount_e4: 190_000,
  created_at: "2026-11-08T09:00:00Z",
  updated_at: "2026-11-08T09:00:00Z"
};

describe("loadBuyerOrders", () => {
  it("keeps the session client language dynamic after login", async () => {
    let language: "zh" | "en" | "km" = "zh";
    const languages: string[] = [];
    const transport: typeof globalThis.fetch = async (input, init) => {
      const request = new Request(input, init);
      languages.push(request.headers.get("Accept-Language") ?? "");
      if (request.url.endsWith("/v1/public/auth/telegram"))
        return success({
          access_token: "access-1",
          access_expires_in: 900,
          refresh_token: "refresh-1",
          refresh_expires_in: 2_592_000,
          account: { public_id: "acct_1", role: "buyer", lang: "zh", status: "active" },
          needs_language: false,
          is_new_account: false
        });
      if (request.url.endsWith("/v1/buyer/me/consents")) return success({ items: [] });
      return success({ items: [], next_cursor: null });
    };
    const session = createTelegramSession({
      end: "buyer",
      baseUrl: "http://api.test",
      initData: () => "query_id=AAA&hash=BBB",
      getLanguage: () => language,
      fetch: transport
    });

    await session.login();
    await loadBuyerOrders(session.client);
    language = "km";
    await loadBuyerOrders(session.client);

    expect(languages).toEqual(["zh", "zh", "zh", "km"]);
  });

  it("uses the newly stored language on the next request of one client", async () => {
    const languages: string[] = [];
    const preferences = new Map<string, string>();
    const storage = {
      getItem: (key: string) => preferences.get(key) ?? null,
      setItem: (key: string, value: string) => preferences.set(key, value)
    };
    const transport: typeof globalThis.fetch = async (input, init) => {
      languages.push(new Request(input, init).headers.get("Accept-Language") ?? "");
      return success({ items: [], next_cursor: null });
    };
    const buyerClient = createBuyerApiClient({
      baseUrl: "http://api.test",
      getLanguage: () => readLanguagePreference(storage) ?? "zh",
      fetch: transport,
      resilience: { retry: { maxAttempts: 1 } }
    });

    storeLanguagePreference("zh", storage);
    await loadBuyerOrders(buyerClient);
    storeLanguagePreference("km", storage);
    await loadBuyerOrders(buyerClient);

    expect(languages).toEqual(["zh", "km"]);
  });

  it("reads the current language for every request", async () => {
    let language: "zh" | "en" | "km" = "zh";
    const languages: string[] = [];
    const transport: typeof globalThis.fetch = async (input, init) => {
      languages.push(new Request(input, init).headers.get("Accept-Language") ?? "");
      return success({ items: [], next_cursor: null });
    };
    const buyerClient = createBuyerApiClient({
      baseUrl: "http://api.test",
      getLanguage: () => language,
      fetch: transport,
      resilience: { retry: { maxAttempts: 1 } }
    });

    await loadBuyerOrders(buyerClient);
    language = "km";
    await loadBuyerOrders(buyerClient);

    expect(languages).toEqual(["zh", "km"]);
  });

  it("loads one page and maps server money without exposing internal IDs", async () => {
    const second = { ...apiOrder, public_id: "01JORDER2" };
    const { urls, transport } = recordingFetch((call) =>
      call === 0
        ? success({ items: [apiOrder], next_cursor: "next-page" })
        : success({ items: [second], next_cursor: null })
    );

    await expect(loadBuyerOrders(client(transport))).resolves.toMatchObject({
      items: [{
        publicId: "01JORDER",
        totalE4: 190_000n,
        subOrders: [{
          totalE4: 190_000n,
          merchantName: { zh: "", en: "", km: "" },
          lines: [{ name: { zh: "", en: "", km: "" } }]
        }]
      }],
      nextCursor: "next-page"
    });
    expect(urls).toEqual(["http://api.test/v1/buyer/orders?page_size=20"]);
  });

  it("keeps a malformed list row visible without hiding valid orders", async () => {
    const warning = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const malformed = { ...apiOrder, public_id: "01JBAD", payable_amount_e4: 101 };
    const { transport } = recordingFetch(() =>
      success({ items: [malformed, apiOrder], next_cursor: null })
    );

    await expect(loadBuyerOrders(client(transport))).resolves.toMatchObject({
      items: [{ dataError: true, publicId: "data-error:01JBAD:0" }, { publicId: "01JORDER" }],
      nextCursor: null
    });
    expect(warning).toHaveBeenCalledWith("[buyer] malformed order", "01JBAD");
  });

  it("rejects inconsistent parent and suborder totals as malformed data", async () => {
    const warning = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const malformed = { ...apiOrder, payable_amount_e4: 200_000 };
    const { transport } = recordingFetch(() =>
      success({ items: [malformed], next_cursor: null })
    );

    await expect(loadBuyerOrders(client(transport))).resolves.toMatchObject({
      items: [{ dataError: true, publicId: "data-error:01JORDER:0" }]
    });
    expect(warning).toHaveBeenCalledWith("[buyer] malformed order", "01JORDER");
  });

  it("rejects inconsistent line and suborder totals as malformed data", async () => {
    const warning = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const malformed = {
      ...apiOrder,
      suborders: [{
        ...apiOrder.suborders[0],
        goods_amount_e4: 180_000,
        payable_amount_e4: 180_000
      }]
    };
    const { transport } = recordingFetch(() =>
      success({ items: [malformed], next_cursor: null })
    );

    await expect(loadBuyerOrders(client(transport))).resolves.toMatchObject({
      items: [{ dataError: true, publicId: "data-error:01JORDER:0" }]
    });
    expect(warning).toHaveBeenCalledWith("[buyer] malformed order", "01JORDER");
  });

  it("maps unknown server statuses to the localized processing fallback", async () => {
    const unknown = {
      ...apiOrder,
      payment_status: "awaiting_review",
      suborders: [{ ...apiOrder.suborders[0], status: "queued" }]
    };
    const { transport } = recordingFetch(() => success({ items: [unknown], next_cursor: null }));

    await expect(loadBuyerOrders(client(transport))).resolves.toMatchObject({
      items: [{ paymentStatus: "unknown", subOrders: [{ status: "unknown" }] }]
    });
  });

  it("loads one detail and preserves a server error code", async () => {
    const { urls, transport } = recordingFetch((call) =>
      call === 0 ? success(apiOrder) : failure(2008, 403)
    );
    await expect(loadBuyerOrder(client(transport), "01JORDER")).resolves.toMatchObject({
      publicId: "01JORDER",
      goodsAmountE4: 190_000n,
      shippingAmountE4: 0n
    });
    await expect(loadBuyerOrder(client(transport), "01JORDER")).rejects.toMatchObject({
      name: "ApiError",
      code: 2008,
      messageKey: "error.2008"
    });
    expect(urls).toEqual([
      "http://api.test/v1/buyer/orders/01JORDER",
      "http://api.test/v1/buyer/orders/01JORDER"
    ]);
  });
});
