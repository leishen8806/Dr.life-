import { describe, expect, it } from "vitest";
import { createApiClient } from "@drlife/api-client";
import { createMerchantProductApi } from "./api";
import type { Product, ProductCreateRequest } from "./form";

const publicId = "01HZY0000000000000000MERCH";

const product: Product = {
  public_id: publicId,
  category_code: "rice",
  name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
  unit: "bag",
  origin: "cn",
  status: "draft",
  source: "manual",
  tiers: [
    { min_qty: "10", unit_price_e4: 125000 },
    { min_qty: "100", unit_price_e4: 118000 }
  ],
  skus: [],
  updated_at: "2026-10-29T00:00:00Z"
};

const createRequest: ProductCreateRequest = {
  category_code: "rice",
  name: product.name,
  unit: "bag",
  origin: "cn",
  lead_days: null,
  vol_m3: null,
  source: "manual",
  tiers: product.tiers
};

interface Call {
  url: string;
  method: string;
  idempotencyKey: string | null;
  body: string;
}

function stub(payload: unknown, status = 200) {
  const calls: Call[] = [];
  const fetch: typeof globalThis.fetch = async (input, init) => {
    const request = new Request(input, init);
    calls.push({
      url: request.url,
      method: request.method,
      idempotencyKey: request.headers.get("Idempotency-Key"),
      body: await request.text()
    });
    return new Response(JSON.stringify(payload), {
      status,
      headers: { "Content-Type": "application/json" }
    });
  };
  const api = createMerchantProductApi(
    createApiClient({
      baseUrl: "http://api.test",
      fetch,
      getToken: () => "token",
      resilience: { retry: { maxAttempts: 1 } }
    })
  );
  return { api, calls };
}

function one(payload: unknown, status = 200) {
  return stub({ ok: true, data: payload, request_id: "server_1" }, status);
}

function page(items: Product[]) {
  return one({ items, page: 1, page_size: 20, total: items.length });
}

describe("createMerchantProductApi", () => {
  it("lists every status when no filter is applied", async () => {
    const { api, calls } = page([product]);

    await expect(api.list("all")).resolves.toEqual([product]);
    expect(calls[0]?.method).toBe("GET");
    expect(calls[0]?.url).toBe("http://api.test/v1/merchant/products");
  });

  it("passes a single status filter through as a query parameter", async () => {
    const { api, calls } = page([]);

    await expect(api.list("on")).resolves.toEqual([]);
    expect(calls[0]?.url).toBe("http://api.test/v1/merchant/products?status=on");
  });

  it("creates a draft product with an idempotency key", async () => {
    const { api, calls } = one(product);

    await expect(api.create(createRequest)).resolves.toEqual(product);
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe("http://api.test/v1/merchant/products");
    expect(calls[0]?.idempotencyKey).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(JSON.parse(calls[0]?.body ?? "null")).toEqual(createRequest);
  });

  it("lowercases the public id because the gateway rejects uppercase path segments", async () => {
    const { api, calls } = one(product);

    await api.update(publicId, { status: "off" });

    expect(calls[0]?.method).toBe("PATCH");
    expect(calls[0]?.url).toBe(
      `http://api.test/v1/merchant/products/${publicId.toLowerCase()}`
    );
    expect(calls[0]?.idempotencyKey).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(JSON.parse(calls[0]?.body ?? "null")).toEqual({ status: "off" });
  });

  it("submits a product for listing on the dedicated publish endpoint", async () => {
    const { api, calls } = one({ ...product, status: "on" });

    await expect(api.publish(publicId)).resolves.toMatchObject({ status: "on" });
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe(
      `http://api.test/v1/merchant/products/${publicId.toLowerCase()}/publish`
    );
    expect(calls[0]?.idempotencyKey).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
  });

  it("surfaces a rejected listing as a typed ApiError", async () => {
    const { api } = stub(
      {
        ok: false,
        error: {
          code: 1002,
          message: { zh: "internal", en: "internal", km: "internal" }
        },
        request_id: "server_2"
      },
      400
    );

    await expect(api.publish(publicId)).rejects.toMatchObject({
      code: 1002,
      kind: "validation",
      requestId: "server_2",
      status: 400
    });
  });

  it("asks for the full category page the gateway allows", async () => {
    const { api, calls } = one({
      items: [
        {
          public_id: "01HZY0000000000000000CATEG",
          code: "rice",
          name: { zh: "米", en: "Rice", km: "អង្ករ" },
          enabled: true,
          sort: 1
        }
      ],
      page: 1,
      page_size: 100,
      total: 1
    });

    await expect(api.listCategories()).resolves.toMatchObject([{ code: "rice" }]);
    expect(calls[0]?.url).toBe(
      "http://api.test/v1/public/categories?page_size=100"
    );
  });

  it("refuses to invent data when the server answers without an envelope", async () => {
    const { api } = stub("<html>gateway</html>", 502);

    await expect(api.list("all")).rejects.toThrow();
  });
});
