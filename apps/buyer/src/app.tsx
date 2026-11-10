import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { type TranslationKey, useI18n } from "@drlife/i18n";
import { LanguageSwitcher, useBackButton } from "@drlife/ui";
import {
  type BuyerApiClient,
  loadCategories,
  loadProducts,
  loadBuyerOrders,
  loadBuyerOrder
} from "./api";
import { appendFeed, firstFeed, nextPage, type ProductFeed } from "./feed";
import type { BuyerOrder } from "./orders";
import { addProduct, cartCount, stepQuantity, updateQuantity, type CartItem } from "./cart";
import { errorMessageKey, type Resource } from "./resource";
import type { Category, Product } from "./types";
import { CategoryView } from "./views/category-view";
import { CartView } from "./views/cart-view";
import { HomeView } from "./views/home-view";
import { ProductDetailView } from "./views/product-detail-view";
import { OrderDetailView } from "./views/order-detail-view";
import { OrderListView } from "./views/order-list-view";

type Route =
  | { name: "home" }
  | { name: "category"; code?: string }
  | { name: "product"; product: Product }
  | { name: "orders" }
  | { name: "order"; order: BuyerOrder }
  | { name: "cart" };

const featuredPageSize = 6;

/** Loads one list and exposes the retry the error state offers. */
function useLoaded<T>(
  load: (signal: AbortSignal) => Promise<T>
): [Resource<T>, () => void] {
  const [resource, setResource] = useState<Resource<T>>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    load(controller.signal).then(
      (value) => {
        if (!controller.signal.aborted) setResource({ status: "ready", value });
      },
      (error: unknown) => {
        if (!controller.signal.aborted)
          setResource({ status: "error", messageKey: errorMessageKey(error) });
      }
    );
    return () => controller.abort();
  }, [load, attempt]);

  const retry = useCallback(() => {
    setResource({ status: "loading" });
    setAttempt((value) => value + 1);
  }, []);
  return [resource, retry];
}

/**
 * A language switch gives the client a new `Accept-Language`, which reloads the
 * catalogue. The lists keep showing the previous data until the reload lands.
 */
type FeedLoader = (signal: AbortSignal) => Promise<ProductFeed>;

/** The pages appended after the first, tied to the loader that produced them. */
interface MorePages {
  loader: FeedLoader;
  feed: ProductFeed;
  loading: boolean;
  messageKey?: TranslationKey;
}

/**
 * One category list, page by page: the first page comes from the loader above,
 * the next ones are appended. A failed next page keeps the rows on screen.
 * Pages of a previous category or language are ignored rather than cleared, so
 * nothing has to be reset from an effect.
 *
 * Exported for `feed-hook.test.tsx`: both guards below are out of reach of the
 * rendered markup, so they are driven through the hook itself.
 */
export function useProductFeed(client: BuyerApiClient, code?: string) {
  const loader = useCallback<FeedLoader>(
    (signal) =>
      loadProducts(client, { category: code }, signal).then(firstFeed),
    [client, code]
  );
  const [first, retryFirst] = useLoaded(loader);
  const [more, setMore] = useState<MorePages>();
  const current = more?.loader === loader ? more : undefined;

  const resource = useMemo<Resource<ProductFeed>>(
    () =>
      current && first.status === "ready"
        ? { status: "ready", value: current.feed }
        : first,
    [current, first]
  );

  const retry = useCallback(() => {
    setMore(undefined);
    retryFirst();
  }, [retryFirst]);

  const loadMore = useCallback(() => {
    if (resource.status !== "ready") return;
    if (current?.loading) return;
    const feed = resource.value;
    const requested = nextPage(feed);
    if (requested === undefined) return;
    setMore({ loader, feed, loading: true });
    loadProducts(client, { category: code, page: requested }).then(
      (page) =>
        setMore({ loader, feed: appendFeed(feed, page), loading: false }),
      (error: unknown) =>
        setMore({
          loader,
          feed,
          loading: false,
          messageKey: errorMessageKey(error)
        })
    );
  }, [client, code, current?.loading, loader, resource]);

  return {
    resource,
    retry,
    loadMore,
    loadingMore: current?.loading ?? false,
    moreMessageKey: current?.messageKey
  };
}

function CategoryPage({
  client,
  categories,
  code,
  actions,
  onSelectCategory,
  onSelectProduct,
  onBack
}: {
  client: BuyerApiClient;
  categories: readonly Category[];
  code?: string;
  actions: ReactNode;
  onSelectCategory: (code?: string) => void;
  onSelectProduct: (product: Product) => void;
  onBack: () => void;
}) {
  const { language, t } = useI18n();
  const { resource, retry, loadMore, loadingMore, moreMessageKey } =
    useProductFeed(client, code);

  return (
    <CategoryView
      t={t}
      language={language}
      categories={categories}
      activeCode={code}
      products={resource}
      loadingMore={loadingMore}
      moreMessageKey={moreMessageKey}
      actions={actions}
      onSelectCategory={onSelectCategory}
      onSelectProduct={onSelectProduct}
      onRetry={retry}
      onLoadMore={loadMore}
      onBack={onBack}
    />
  );
}

export function BuyerApp({ isTelegram, client }: { isTelegram: boolean; client: BuyerApiClient }) {
  const { language, t } = useI18n();

  const [stack, setStack] = useState<Route[]>([{ name: "home" }]);
  const [cart, setCart] = useState<CartItem[]>([]);
  const [cartError, setCartError] = useState<ReturnType<typeof addProduct>["error"]>();
  const route = stack[stack.length - 1] ?? { name: "home" };
  const push = useCallback(
    (next: Route) => setStack((current) => [...current, next]),
    []
  );
  const replace = useCallback(
    (next: Route) => setStack((current) => [...current.slice(0, -1), next]),
    []
  );
  const back = useCallback(
    () => setStack((current) => (current.length > 1 ? current.slice(0, -1) : current)),
    []
  );
  useBackButton(back, isTelegram && stack.length > 1);

  const loadAllCategories = useCallback(
    (signal: AbortSignal) => loadCategories(client, signal),
    [client]
  );
  const [categories, retryCategories] = useLoaded(loadAllCategories);
  const loadFeatured = useCallback(
    async (signal: AbortSignal) =>
      (await loadProducts(client, { pageSize: featuredPageSize }, signal)).items,
    [client]
  );
  const [featured, retryFeatured] = useLoaded(loadFeatured);

  const openCategory = useCallback(
    (code?: string) => push({ name: "category", code }),
    [push]
  );
  const switchCategory = useCallback(
    (code?: string) => replace({ name: "category", code }),
    [replace]
  );
  const openProduct = useCallback(
    (product: Product) => push({ name: "product", product }),
    [push]
  );
  const openOrders = useCallback(() => push({ name: "orders" }), [push]);
  const loadOrders = useCallback(
    (signal: AbortSignal) => loadBuyerOrders(client, undefined, signal),
    [client]
  );
  const [orders, retryOrders] = useLoaded(loadOrders);
  const [moreOrders, setMoreOrders] = useState<{
    items: BuyerOrder[];
    nextCursor: string | null;
    loading: boolean;
    messageKey?: ReturnType<typeof errorMessageKey>;
  }>();
  const loadMoreOrders = useCallback(() => {
    if (orders.status !== "ready" || moreOrders?.loading) return;
    const currentItems = moreOrders?.items ?? orders.value.items;
    const cursor = moreOrders?.nextCursor ?? orders.value.nextCursor;
    if (!cursor) return;
    setMoreOrders({ items: currentItems, nextCursor: cursor, loading: true });
    loadBuyerOrders(client, cursor).then(
      (page) => {
        setMoreOrders({
          items: [...currentItems, ...page.items],
          nextCursor: page.nextCursor,
          loading: false
        });
      },
      (error: unknown) =>
        setMoreOrders({
          items: currentItems,
          nextCursor: cursor,
          loading: false,
          messageKey: errorMessageKey(error)
        })
    );
  }, [client, moreOrders, orders]);
  const retryOrderList = useCallback(() => {
    setMoreOrders(undefined);
    retryOrders();
  }, [retryOrders]);
  const orderList =
    orders.status === "ready"
      ? {
          status: "ready" as const,
          value: moreOrders?.items ?? orders.value.items
        }
      : orders;
  const nextOrderCursor =
    moreOrders?.nextCursor ?? (orders.status === "ready" ? orders.value.nextCursor : null);
  const openOrder = useCallback((order: BuyerOrder) => {
    push({ name: "order", order });
    loadBuyerOrder(client, order.publicId).then((detail) =>
      setStack((current) => {
        const route = current.at(-1);
        return route?.name === "order" && route.order.publicId === order.publicId
          ? [...current.slice(0, -1), { name: "order", order: detail }]
          : current;
      })
    ).catch(() => undefined);
  }, [client, push]);

  const addToCart = useCallback((product: Product) => {
    const result = addProduct(cart, product);
    setCart(result.cart);
    setCartError(result.error);
  }, [cart]);
  const openCart = useCallback(() => push({ name: "cart" }), [push]);
  const updateCartQuantity = useCallback((item: CartItem, quantity: string) => {
    const result = updateQuantity(cart, item.product.public_id, quantity);
    setCart(result.cart);
    setCartError(result.error);
  }, [cart]);
  const stepCartQuantity = useCallback((item: CartItem, delta: -1 | 1) => {
    const result = stepQuantity(cart, item.product.public_id, delta);
    setCart(result.cart);
    setCartError(result.error);
  }, [cart]);
  const removeFromCart = useCallback((item: CartItem) => {
    setCart((current) => current.filter((entry) => entry.product.public_id !== item.product.public_id));
    setCartError(undefined);
  }, []);

  const actions = (
    <>
      <LanguageSwitcher />
      <button type="button" className="buyer-orders-action" onClick={openOrders}>
        {t("order.pageTitle")}
      </button>
      <button type="button" className="buyer-cart-action" onClick={openCart}>
        {t("mall.cart")} <span aria-label={t("mall.cartCount", { count: cartCount(cart) })}>{cartCount(cart)}</span>
      </button>
    </>
  );
  const knownCategories = categories.status === "ready" ? categories.value : [];

  if (route.name === "order")
    return (
      <OrderDetailView
        t={t}
        language={language}
        order={route.order}
        actions={actions}
        onBack={back}
      />
    );

  if (route.name === "orders")
    return (
      <OrderListView
        t={t}
        language={language}
        orders={orderList}
        actions={actions}
        onBack={back}
        onRetry={retryOrderList}
        onOpen={openOrder}
        onLoadMore={nextOrderCursor ? loadMoreOrders : undefined}
        loadingMore={moreOrders?.loading ?? false}
        moreMessageKey={moreOrders?.messageKey}
      />
    );

  if (route.name === "cart")
    return (
      <CartView
        t={t}
        language={language}
        items={cart}
        error={cartError}
        actions={actions}
        onBack={back}
        onUpdateQuantity={updateCartQuantity}
        onStepQuantity={stepCartQuantity}
        onRemove={removeFromCart}
      />
    );

  if (route.name === "product")
    return (
      <ProductDetailView
        t={t}
        language={language}
        product={route.product}
        actions={actions}
        onBack={back}
        onAddToCart={() => addToCart(route.product)}
      />
    );

  if (route.name === "category")
    return (
      <CategoryPage
        key={route.code ?? "all"}
        client={client}
        categories={knownCategories}
        code={route.code}
        actions={actions}
        onSelectCategory={switchCategory}
        onSelectProduct={openProduct}
        onBack={back}
      />
    );

  return (
    <HomeView
      t={t}
      language={language}
      categories={categories}
      featured={featured}
      actions={actions}
      onRetryCategories={retryCategories}
      onRetryFeatured={retryFeatured}
      onSelectCategory={openCategory}
      onSelectProduct={openProduct}
    />
  );
}
