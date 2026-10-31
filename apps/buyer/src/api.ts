import { type ApiClientOptions, createApiClient } from "@drlife/api-client";
import type { Language } from "@drlife/i18n";
import type { Category, ProductPage } from "./types";

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
  /** One-based, as the API counts pages. Omitted means the first page. */
  page?: number;
  pageSize?: number;
}

const categoryPageSize = 100;
/** The catalogue is bounded, but a runaway total must not loop forever. */
const maxCategoryPages = 20;
export const productPageSize = 20;

/**
 * The category rail and the home grid show every category, so the pages after
 * the first are fetched too; `page_size` is capped at 100 by the API.
 */
export async function loadCategories(
  client: BuyerApiClient,
  signal?: AbortSignal
): Promise<Category[]> {
  const categories: Category[] = [];
  for (let page = 1; page <= maxCategoryPages; page += 1) {
    const { data } = await client.GET("/v1/public/categories", {
      params: {
        query: { page: page === 1 ? undefined : page, page_size: categoryPageSize }
      },
      signal
    });
    if (!data) throw new Error("The category response was malformed");
    categories.push(...data.data.items);
    if (!data.data.items.length || categories.length >= data.data.total) break;
  }
  return categories;
}

/** Returns the whole envelope: the count label and paging need `total`. */
export async function loadProducts(
  client: BuyerApiClient,
  query: ProductQuery,
  signal?: AbortSignal
): Promise<ProductPage> {
  const { data } = await client.GET("/v1/public/products", {
    params: {
      query: {
        page: query.page,
        page_size: query.pageSize ?? productPageSize,
        category: query.category
      }
    },
    signal
  });
  if (!data) throw new Error("The product response was malformed");
  return data.data;
}
