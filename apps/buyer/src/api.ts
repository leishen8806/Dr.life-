import { type ApiClientOptions, createApiClient } from "@drlife/api-client";
import type { Language } from "@drlife/i18n";
import type { Category, ProductPage } from "./types";
import type { BuyerOrder, BuyerSubOrder, OrderLine, OrderStatus, PaymentStatus } from "./orders";
import type { components } from "@drlife/api-client";

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

const maxOrderPages = 20;

function idText(value: string) {
  return { zh: value, en: value, km: value };
}

function money(value: number): bigint {
  return BigInt(value);
}

type ApiSuborder = components["schemas"]["BuyerSuborder"];
type ApiOrder = components["schemas"]["BuyerOrder"] | components["schemas"]["BuyerOrderSummary"];

function mapSuborder(suborder: ApiSuborder): BuyerSubOrder {
  const lines: OrderLine[] = suborder.items.map((item, index) => ({
    publicId: `${suborder.public_id}-${item.sku_public_id}-${index}`,
    name: idText(item.sku_public_id),
    quantity: String(item.quantity),
    unit: "item",
    unitPriceE4: money(item.unit_price_e4),
    totalE4: money(item.line_total_e4)
  }));
  return {
    publicId: suborder.public_id,
    merchantName: idText(suborder.merchant_public_id),
    status: suborder.status as OrderStatus,
    lines,
    totalE4: money(suborder.payable_amount_e4),
    shippingAmountE4: money(suborder.shipping_amount_e4),
    shippingQuoteExpiresAt: suborder.shipping_quote_expires_at,
    events: []
  };
}

function mapOrder(order: ApiOrder): BuyerOrder {
  return {
    publicId: order.public_id,
    createdAt: order.created_at,
    totalE4: money(order.payable_amount_e4),
    goodsAmountE4: money(order.goods_amount_e4),
    shippingAmountE4: money(order.shipping_amount_e4),
    paymentStatus: order.payment_status as PaymentStatus,
    subOrders: order.suborders.map(mapSuborder)
  };
}

export async function loadBuyerOrders(
  client: BuyerApiClient,
  signal?: AbortSignal
): Promise<BuyerOrder[]> {
  const orders: BuyerOrder[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < maxOrderPages; page += 1) {
    const { data } = await client.GET("/v1/buyer/orders", {
      params: { query: { cursor, page_size: 100 } },
      signal
    });
    if (!data) throw new Error("The order response was malformed");
    orders.push(...data.data.items.map(mapOrder));
    cursor = data.data.next_cursor ?? undefined;
    if (!cursor) break;
  }
  return orders;
}

export async function loadBuyerOrder(
  client: BuyerApiClient,
  publicId: string,
  signal?: AbortSignal
): Promise<BuyerOrder> {
  const { data } = await client.GET("/v1/buyer/orders/{public_id}", {
    params: { path: { public_id: publicId } },
    signal
  });
  if (!data) throw new Error("The order response was malformed");
  return mapOrder(data.data);
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
