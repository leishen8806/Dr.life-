import { ApiError, type ApiClientOptions, createApiClient } from "@drlife/api-client";
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

export const buyerOrderPageSize = 20;

const emptyLocalizedText = { zh: "", en: "", km: "" };

function money(value: number): bigint {
  if (!Number.isSafeInteger(value) || value < 0 || value % 100 !== 0)
    throw new ApiError(1001, "order-money", 502);
  return BigInt(value);
}

type ApiSuborder = components["schemas"]["BuyerSuborder"];
type ApiOrder = components["schemas"]["BuyerOrder"] | components["schemas"]["BuyerOrderSummary"];

const orderStatuses = new Set<OrderStatus>([
  "awaiting_payment",
  "paid",
  "submitted",
  "accepted",
  "in_fulfillment",
  "shipped",
  "delivered_verified",
  "settled",
  "rejected",
  "cancelled",
  "delivery_failed",
  "returning",
  "returned",
  "exception",
  "mixed"
]);

const paymentStatuses = new Set<PaymentStatus>([
  "pending",
  "confirming",
  "succeeded",
  "failed",
  "refunded"
]);

export class UnknownBuyerStatusError extends Error {
  constructor(kind: "order" | "payment", value: string) {
    super(`[buyer] unknown ${kind} status: ${value}`);
    this.name = "UnknownBuyerStatusError";
  }
}

function strictBuyerContractChecks(): boolean {
  return import.meta.env.DEV || import.meta.env.MODE === "test";
}

function mapOrderStatus(value: string): OrderStatus {
  if (orderStatuses.has(value as OrderStatus)) return value as OrderStatus;
  if (strictBuyerContractChecks()) throw new UnknownBuyerStatusError("order", value);
  console.warn("[buyer] unknown order status", value);
  return "unknown";
}

function mapPaymentStatus(value: string): PaymentStatus {
  if (paymentStatuses.has(value as PaymentStatus)) return value as PaymentStatus;
  if (strictBuyerContractChecks()) throw new UnknownBuyerStatusError("payment", value);
  console.warn("[buyer] unknown payment status", value);
  return "unknown";
}

function mapSuborder(suborder: ApiSuborder): BuyerSubOrder {
  // Follow-up contract task: map localized merchant/item fields once OpenAPI exposes them.
  const lines: OrderLine[] = suborder.items.map((item, index) => ({
    publicId: `${suborder.public_id}-${item.sku_public_id}-${index}`,
    name: emptyLocalizedText,
    quantity: String(item.quantity),
    unit: "item",
    unitPriceE4: money(item.unit_price_e4),
    totalE4: money(item.line_total_e4)
  }));
  const goodsAmountE4 = money(suborder.goods_amount_e4);
  const shippingAmountE4 = money(suborder.shipping_amount_e4);
  const totalE4 = money(suborder.payable_amount_e4);
  const lineGoodsE4 = lines.reduce((sum, item) => sum + item.totalE4, 0n);
  // The OpenAPI/database contract guarantees payable = goods + shipping and
  // line totals = goods totals; reject a response that violates that contract.
  if (goodsAmountE4 !== lineGoodsE4 || totalE4 !== goodsAmountE4 + shippingAmountE4)
    throw new ApiError(1001, "order-money", 502);
  return {
    publicId: suborder.public_id,
    merchantName: emptyLocalizedText,
    status: mapOrderStatus(suborder.status),
    lines,
    goodsAmountE4,
    totalE4,
    shippingAmountE4,
    shippingQuoteExpiresAt: suborder.shipping_quote_expires_at,
    events: []
  };
}

function mapOrder(order: ApiOrder): BuyerOrder {
  const subOrders = order.suborders.map(mapSuborder);
  const goodsAmountE4 = money(order.goods_amount_e4);
  const shippingAmountE4 = money(order.shipping_amount_e4);
  const totalE4 = money(order.payable_amount_e4);
  const suborderGoodsE4 = subOrders.reduce((sum, subOrder) => sum + subOrder.goodsAmountE4, 0n);
  const suborderShippingE4 = subOrders.reduce((sum, subOrder) => sum + subOrder.shippingAmountE4, 0n);
  const suborderTotalE4 = subOrders.reduce((sum, subOrder) => sum + subOrder.totalE4, 0n);
  // The OpenAPI/database contract guarantees the same invariants at parent level
  // and that the parent equals the sum of its merchant suborders.
  if (
    goodsAmountE4 !== suborderGoodsE4 ||
    shippingAmountE4 !== suborderShippingE4 ||
    totalE4 !== goodsAmountE4 + shippingAmountE4 ||
    totalE4 !== suborderTotalE4
  ) throw new ApiError(1001, "order-money", 502);
  return {
    publicId: order.public_id,
    createdAt: order.created_at,
    totalE4,
    goodsAmountE4,
    shippingAmountE4,
    paymentStatus: mapPaymentStatus(order.payment_status),
    subOrders
  };
}

function mapOrderForList(order: ApiOrder, index: number): BuyerOrder {
  try {
    return mapOrder(order);
  } catch (error) {
    if (error instanceof ApiError && error.code === 1001) {
      // Keep a non-sensitive public identifier visible in production diagnostics
      // until the app has a unified error-reporting sink.
      console.warn("[buyer] malformed order", order.public_id);
      return {
        publicId: `data-error:${order.public_id}:${index}`,
        dataErrorPublicId: order.public_id,
        createdAt: "",
        totalE4: 0n,
        goodsAmountE4: 0n,
        shippingAmountE4: 0n,
        paymentStatus: "unknown",
        subOrders: [],
        dataError: true
      };
    }
    throw error;
  }
}

export interface BuyerOrderPage {
  items: BuyerOrder[];
  nextCursor: string | null;
}

export async function loadBuyerOrders(
  client: BuyerApiClient,
  cursor?: string,
  signal?: AbortSignal
): Promise<BuyerOrderPage> {
  const { data } = await client.GET("/v1/buyer/orders", {
    params: { query: { cursor, page_size: buyerOrderPageSize } },
    signal
  });
  if (!data) throw new ApiError(1001, "buyer-orders", 502);
  return {
    items: data.data.items.map((order, index) => mapOrderForList(order, index)),
    nextCursor: data.data.next_cursor ?? null
  };
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
  if (!data) throw new ApiError(1001, "buyer-order", 502);
  return mapOrder(data.data);
}

export interface ProductQuery {
  category?: string;
  /** One-based, as the API counts pages. Omitted means the first page. */
  page?: number;
  pageSize?: number;
}

export type ProductSearchSort = "relevance" | "price_asc" | "price_desc" | "newest";

export interface ProductSearchQuery {
  q: string;
  category?: string;
  minPriceE4?: number;
  maxPriceE4?: number;
  sort?: ProductSearchSort;
  cursor?: string;
  pageSize?: number;
}

export type ProductSearchData = components["schemas"]["SearchProductData"];

export async function searchProducts(
  client: BuyerApiClient,
  query: ProductSearchQuery,
  signal?: AbortSignal
): Promise<ProductSearchData> {
  const { data } = await client.GET("/v1/public/search/products", {
    params: {
      query: {
        q: query.q,
        category: query.category,
        min_price_e4: query.minPriceE4,
        max_price_e4: query.maxPriceE4,
        sort: query.sort,
        cursor: query.cursor,
        page_size: query.pageSize ?? productPageSize
      }
    },
    signal
  });
  if (!data) throw new ApiError(1001, "search-products", 502);
  return data.data;
}

export async function suggestProducts(
  client: BuyerApiClient,
  query: string,
  signal?: AbortSignal
): Promise<string[]> {
  const { data } = await client.GET("/v1/public/search/suggest", {
    params: { query: { q: query } },
    signal
  });
  if (!data) throw new ApiError(1001, "search-suggest", 502);
  return data.data.items.map((item) => item.text);
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
    if (!data) throw new ApiError(1001, "categories", 502);
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
  if (!data) throw new ApiError(1001, "products", 502);
  return data.data;
}
