import { type ApiClientOptions, createApiClient } from "@drlife/api-client";
import type { Language } from "@drlife/i18n";
import type { Category, Product } from "./types";

export type BuyerApiClient = ReturnType<typeof createApiClient>;

export interface BuyerApiOptions {
  baseUrl?: string;
  getLanguage: () => Language;
  fetch?: typeof globalThis.fetch;
  resilience?: ApiClientOptions["resilience"];
}

/** The catalogue is public, so this client carries no token and no step-up. */
export function createBuyerApiClient(options: BuyerApiOptions): BuyerApiClient {
  return createApiClient(options);
}

export interface ProductQuery {
  category?: string;
  pageSize?: number;
}

const categoryPageSize = 100;
const productPageSize = 20;

export async function loadCategories(
  client: BuyerApiClient,
  signal?: AbortSignal
): Promise<Category[]> {
  const { data } = await client.GET("/v1/public/categories", {
    params: { query: { page_size: categoryPageSize } },
    signal
  });
  if (!data) throw new Error("The category response was malformed");
  return data.data.items;
}

export async function loadProducts(
  client: BuyerApiClient,
  query: ProductQuery,
  signal?: AbortSignal
): Promise<Product[]> {
  const { data } = await client.GET("/v1/public/products", {
    params: {
      query: {
        page_size: query.pageSize ?? productPageSize,
        category: query.category
      }
    },
    signal
  });
  if (!data) throw new Error("The product response was malformed");
  return data.data.items;
}
