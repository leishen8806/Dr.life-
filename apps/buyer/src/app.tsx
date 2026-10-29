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

/**
 * One category list, page by page: the first page replaces the list, the next
 * ones are appended. A failed next page keeps the rows already on screen.
 */
function useProductFeed(client: BuyerApiClient, code?: string) {
  const [resource, setResource] = useState<Resource<ProductFeed>>({
    status: "loading"
  });
  const [attempt, setAttempt] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreMessageKey, setMoreMessageKey] = useState<TranslationKey>();

  useEffect(() => {
    const controller = new AbortController();
    setResource({ status: "loading" });
    setLoadingMore(false);
    setMoreMessageKey(undefined);
    loadProducts(client, { category: code }, controller.signal).then(
      (page) => {
        if (!controller.signal.aborted)
          setResource({ status: "ready", value: firstFeed(page) });
      },
      (error: unknown) => {
        if (!controller.signal.aborted)
          setResource({ status: "error", messageKey: errorMessageKey(error) });
      }
    );
    return () => controller.abort();
  }, [client, code, attempt]);

  const retry = useCallback(() => setAttempt((value) => value + 1), []);

  const loadMore = useCallback(() => {
    if (resource.status !== "ready" || !resource.value.hasMore || loadingMore)
      return;
    const current = resource.value;
    setLoadingMore(true);
    setMoreMessageKey(undefined);
    loadProducts(client, { category: code, page: current.page + 1 }).then(
      (page) => {
        setResource({ status: "ready", value: appendFeed(current, page) });
        setLoadingMore(false);
      },
      (error: unknown) => {
        setMoreMessageKey(errorMessageKey(error));
        setLoadingMore(false);
      }
    );
  }, [client, code, loadingMore, resource]);

  return { resource, retry, loadMore, loadingMore, moreMessageKey };
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
