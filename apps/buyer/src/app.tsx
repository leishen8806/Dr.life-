import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { type Language, type TranslationKey, useI18n } from "@drlife/i18n";
import { LanguageSwitcher, useBackButton } from "@drlife/ui";
import {
  type BuyerApiClient,
  createBuyerApiClient,
  loadCategories,
  loadProducts
} from "./api";
import { appendFeed, firstFeed, type ProductFeed } from "./feed";
import { errorMessageKey, type Resource } from "./resource";
import type { Category, Product } from "./types";
import { CategoryView } from "./views/category-view";
import { HomeView } from "./views/home-view";
import { ProductDetailView } from "./views/product-detail-view";

type Route =
  | { name: "home" }
  | { name: "category"; code?: string }
  | { name: "product"; product: Product };

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
function useBuyerClient(language: Language): BuyerApiClient {
  return useMemo(
    () => createBuyerApiClient({ getLanguage: () => language }),
    [language]
  );
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
 */
function useProductFeed(client: BuyerApiClient, code?: string) {
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
    if (resource.status !== "ready" || !resource.value.hasMore) return;
    if (current?.loading) return;
    const feed = resource.value;
    setMore({ loader, feed, loading: true });
    loadProducts(client, { category: code, page: feed.page + 1 }).then(
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

export function BuyerApp({ isTelegram }: { isTelegram: boolean }) {
  const { language, t } = useI18n();
  const client = useBuyerClient(language);

  const [stack, setStack] = useState<Route[]>([{ name: "home" }]);
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

  const actions = <LanguageSwitcher />;
  const knownCategories = categories.status === "ready" ? categories.value : [];

  if (route.name === "product")
    return (
      <ProductDetailView
        t={t}
        language={language}
        product={route.product}
        actions={actions}
        onBack={back}
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
