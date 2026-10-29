import { describe, expect, it } from "vitest";
import { ApiError } from "@drlife/api-client";
import { createBuyerApiClient, loadCategories, loadProducts } from "./api";
import type { Category, Product } from "./types";

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
    { min_qty: "1", unit_price_e4: 95_000 },
    { min_qty: "100", unit_price_e4: 88_000 }
  ],
  updated_at: "2026-10-29T00:00:00Z"
};

function recordingFetch(response: () => Response) {
  const urls: string[] = [];
  const transport: typeof globalThis.fetch = async (input, init) => {
    urls.push(new Request(input, init).url);
    return response();
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
});

describe("loadProducts", () => {
  it("filters by category code and page size", async () => {
    const { urls, transport } = recordingFetch(() =>
      success({ items: [product], page: 1, page_size: 20, total: 1 })
    );

    await expect(
      loadProducts(client(transport), { category: "c4", pageSize: 20 })
    ).resolves.toEqual([product]);
    expect(urls[0]).toBe(
      "http://api.test/v1/public/products?page_size=20&category=c4"
    );
  });

  it("omits the category filter on the recommended list", async () => {
    const { urls, transport } = recordingFetch(() =>
      success({ items: [], page: 1, page_size: 6, total: 0 })
    );

    await expect(
      loadProducts(client(transport), { pageSize: 6 })
    ).resolves.toEqual([]);
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
