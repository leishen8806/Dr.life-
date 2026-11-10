import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type TranslationKey, useI18n } from "@drlife/i18n";
import { LanguageSwitcher, useBackButton } from "@drlife/ui";
import {
  type BuyerApiClient,
  type BuyerOrderPage,
  loadCategories,
  loadProducts,
  loadBuyerOrders,
  loadBuyerOrder
} from "./api";
import { appendFeed, firstFeed, nextPage, type ProductFeed } from "./feed";
import type { BuyerOrder } from "./orders";
import { createMockPaymentAttempt, createPaymentIdempotencyKey, pendingPaymentAttempt, type PaymentAttempt } from "./payment";
import { addProduct, cartCount, stepQuantity, updateQuantity, type CartItem } from "./cart";
import { errorMessageKey, type Resource } from "./resource";
import type { Category, Product } from "./types";
import { CategoryView } from "./views/category-view";
import { CartView } from "./views/cart-view";
import { HomeView } from "./views/home-view";
import { ProductDetailView } from "./views/product-detail-view";
import { OrderDetailView } from "./views/order-detail-view";
import { OrderListView } from "./views/order-list-view";
import { PaymentView } from "./views/payment-view";

type Route =
  | { name: "home" }
  | { name: "category"; code?: string }
  | { name: "product"; product: Product }
  | { name: "orders" }
  | { name: "order"; publicId: string; order?: BuyerOrder }
  | { name: "payment"; order: BuyerOrder; attempt: PaymentAttempt }
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
export function useProductFeed(client: BuyerApiClient, code?: string, language?: string) {
  const loader = useCallback<FeedLoader>(
    (signal) =>
      loadProducts(client, { category: code }, signal).then(firstFeed),
    [client, code, language]
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

interface MoreOrders {
  language: string;
  items: BuyerOrder[];
  nextCursor: string | null;
  loading: boolean;
  messageKey?: ReturnType<typeof errorMessageKey>;
}

/** Orders are paged independently from the first response and reset on language changes. */
export function useOrderFeed(client: BuyerApiClient, language: string) {
  const [readyLanguage, setReadyLanguage] = useState<string>();
  const loadOrders = useCallback(
    (signal: AbortSignal) =>
      loadBuyerOrders(client, undefined, signal).then((page) => {
        if (!signal.aborted) setReadyLanguage(language);
        return page;
      }),
    [client, language]
  );
  const [orders, retryOrders] = useLoaded(loadOrders);
  const [moreOrders, setMoreOrders] = useState<MoreOrders>();
  const moreOrdersRequest = useRef<AbortController | undefined>(undefined);

  // This reset intentionally synchronizes the continuation cache with the
  // language-keyed first-page loader.
  useEffect(() => {
    moreOrdersRequest.current?.abort();
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setMoreOrders(undefined);
    // The first page is reloaded by useLoaded when the language-keyed loader changes.
  }, [language]);
  useEffect(() => () => moreOrdersRequest.current?.abort(), []);

  const loadMore = useCallback(() => {
    const current = moreOrders?.language === language ? moreOrders : undefined;
    // The old first page remains visible while the language-keyed request is in flight.
    if (orders.status !== "ready" || readyLanguage !== language || current?.loading) return;
    const currentItems = current?.items ?? orders.value.items;
    const cursor = current?.nextCursor ?? orders.value.nextCursor;
    if (!cursor) return;
    moreOrdersRequest.current?.abort();
    const controller = new AbortController();
    moreOrdersRequest.current = controller;
    setMoreOrders({ language, items: currentItems, nextCursor: cursor, loading: true });
    loadBuyerOrders(client, cursor, controller.signal).then(
      (page: BuyerOrderPage) => {
        if (controller.signal.aborted) return;
        setMoreOrders({
          language,
          items: [
            ...currentItems,
            ...page.items.filter((item) => !currentItems.some((existing) =>
              item.dataError
                ? existing.dataErrorPublicId === item.dataErrorPublicId
                : existing.publicId === item.publicId
            ))
          ],
          nextCursor: page.nextCursor,
          loading: false
        });
      },
      (error: unknown) => {
        if (controller.signal.aborted) return;
        setMoreOrders({
          language,
          items: currentItems,
          nextCursor: cursor,
          loading: false,
          messageKey: errorMessageKey(error)
        });
      }
    );
  }, [client, language, moreOrders, orders, readyLanguage]);

  const retry = useCallback(() => {
    moreOrdersRequest.current?.abort();
    setMoreOrders(undefined);
    retryOrders();
  }, [retryOrders]);
  const current = moreOrders?.language === language ? moreOrders : undefined;
  const resource = orders.status === "ready" && readyLanguage === language
    ? { status: "ready" as const, value: current?.items ?? orders.value.items }
    : orders.status === "ready" ? { status: "loading" as const } : orders;
  return {
    resource,
    retry,
    loadMore,
    loadingMore: current?.loading ?? false,
    moreMessageKey: current?.messageKey,
    nextCursor: current?.nextCursor ?? (orders.status === "ready" ? orders.value.nextCursor : null)
  };
}

/** Loads one order detail and reloads it whenever the active language changes. */
export function useOrderDetail(client: BuyerApiClient, publicId: string, language: string) {
  const [loaded, setLoaded] = useState<{ key: string; resource: Resource<BuyerOrder> }>({
    key: "",
    resource: { status: "loading" }
  });
  const request = useRef<AbortController | undefined>(undefined);
  const key = `${publicId}:${language}`;

  const load = useCallback(() => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoaded({ key, resource: { status: "loading" } });
    loadBuyerOrder(client, publicId, controller.signal).then(
      (value) => {
        if (!controller.signal.aborted) setLoaded({ key, resource: { status: "ready", value } });
      },
      (error: unknown) => {
        if (!controller.signal.aborted) setLoaded({ key, resource: { status: "error", messageKey: errorMessageKey(error) } });
      }
    );
  }, [client, key, publicId]);

  useEffect(() => {
    if (!publicId) {
      request.current?.abort();
      return;
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
    return () => request.current?.abort();
  }, [load, language, publicId]);

  return { resource: loaded.key === key ? loaded.resource : { status: "loading" as const }, retry: load };
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
    useProductFeed(client, code, language);

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
  const [paymentAttempts, setPaymentAttempts] = useState<Record<string, PaymentAttempt>>({});
  const [paymentSequences, setPaymentSequences] = useState<Record<string, number>>({});
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
    () => {
      setStack((current) => (current.length > 1 ? current.slice(0, -1) : current));
    },
    []
  );
  useBackButton(back, isTelegram && stack.length > 1);

  const loadAllCategories = useCallback(
    (signal: AbortSignal) => loadCategories(client, signal),
    [client, language]
  );
  const [categories, retryCategories] = useLoaded(loadAllCategories);
  const loadFeatured = useCallback(
    async (signal: AbortSignal) =>
      (await loadProducts(client, { pageSize: featuredPageSize }, signal)).items,
    [client, language]
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
  const { resource: orders, retry: retryOrders, loadMore: loadMoreOrders, loadingMore, moreMessageKey: moreOrdersMessageKey, nextCursor: nextOrderCursor } =
    useOrderFeed(client, language);
  const orderList = orders;
  const openOrder = useCallback((order: BuyerOrder) => {
    push({ name: "order", publicId: order.publicId, order });
  }, [push]);
  const openPayment = useCallback((order: BuyerOrder) => {
    const idempotencyKey = paymentAttempts[order.publicId]?.idempotencyKey ?? createPaymentIdempotencyKey();
    push({
      name: "payment",
      order,
      attempt: paymentAttempts[order.publicId] ?? pendingPaymentAttempt(order, idempotencyKey)
    });
  }, [paymentAttempts, push]);
  const payOrder = useCallback((order: BuyerOrder) => {
    const idempotencyKey = paymentAttempts[order.publicId]?.idempotencyKey ?? createPaymentIdempotencyKey();
    const attemptNumber = (paymentSequences[order.publicId] ?? 0) + 1;
    const attempt = createMockPaymentAttempt(order, idempotencyKey, attemptNumber);
    setPaymentAttempts((current) => ({ ...current, [order.publicId]: attempt }));
    setPaymentSequences((current) => ({ ...current, [order.publicId]: attemptNumber }));
    setStack((current) => current.map((item, index) =>
      index === current.length - 1 && item.name === "payment" ? { ...item, attempt } : item
    ));
    if (isTelegram && typeof window !== "undefined") {
      const openLink = (window as unknown as { Telegram?: { WebApp?: { openLink?: (url: string) => void } } }).Telegram?.WebApp?.openLink;
      openLink?.(attempt.paymentLink!);
    }
  }, [isTelegram, paymentAttempts, paymentSequences]);
  const refreshPayment = useCallback((order: BuyerOrder) => {
    const current = paymentAttempts[order.publicId];
    if (!current || current.status !== "confirming") return;
    const attempt = { ...current, status: "succeeded" as const };
    setPaymentAttempts((items) => ({ ...items, [order.publicId]: attempt }));
    setStack((items) => items.map((item, index) =>
      index === items.length - 1 && item.name === "payment"
        ? { ...item, attempt, order: { ...item.order, paymentStatus: "succeeded" } }
        : item.name === "order" && item.order?.publicId === order.publicId && item.order
          ? { ...item, order: { ...item.order, paymentStatus: "succeeded" } }
          : item
    ));
  }, [paymentAttempts]);
  const retryPayment = useCallback((order: BuyerOrder) => {
    const idempotencyKey = createPaymentIdempotencyKey();
    const attempt = pendingPaymentAttempt(order, idempotencyKey);
    setPaymentAttempts((current) => ({ ...current, [order.publicId]: attempt }));
    setStack((current) => current.map((item, index) =>
      index === current.length - 1 && item.name === "payment" ? { ...item, attempt } : item
    ));
  }, []);
  const orderDetail = useOrderDetail(client, route.name === "order" ? route.publicId : "", language);

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
  const detailOrder = orderDetail.resource.status === "ready" ? orderDetail.resource.value : undefined;
  const openPaymentFromDetail = detailOrder
    ? () => openPayment(detailOrder)
    : undefined;

  if (route.name === "order")
    return (
      <OrderDetailView
        t={t}
        language={language}
        order={orderDetail.resource}
        actions={actions}
        onRetry={orderDetail.retry}
        onBack={back}
        // The current payment flow is an interface stub; never expose it in production
        // until the real KESS endpoint and server-authoritative 409/3006 handling exist.
        onPay={import.meta.env.DEV ? openPaymentFromDetail : undefined}
      />
    );

  if (route.name === "payment")
    return (
      <PaymentView
        t={t}
        language={language}
        order={route.order}
        attempt={route.attempt}
        actions={actions}
        onBack={back}
        onPay={() => payOrder(route.order)}
        onRetry={() => retryPayment(route.order)}
        onRefresh={() => refreshPayment(route.order)}
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
        onRetry={retryOrders}
        onOpen={openOrder}
        onLoadMore={nextOrderCursor ? loadMoreOrders : undefined}
        loadingMore={loadingMore}
        moreMessageKey={moreOrdersMessageKey}
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
