import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { AppHeader, Card } from "@drlife/ui";
import type { Language, TranslationKey } from "@drlife/i18n";
import type { BuyerApiClient, ProductSearchData, ProductSearchQuery, ProductSearchSort } from "../api";
import { searchProducts, suggestProducts } from "../api";
import { validatePriceRange } from "../search";
import { errorMessageKey, type Resource } from "../resource";
import type { Category } from "../types";
import { localized } from "../localized";
import type { LocaleProps } from "./common";
import { ProductList } from "./product-list";

export interface SearchDraft {
  q: string;
  category?: string;
  minPrice: string;
  maxPrice: string;
  sort: ProductSearchSort;
}

const initialDraft: SearchDraft = { q: "", minPrice: "", maxPrice: "", sort: "relevance" };

export function useProductSearch(client: BuyerApiClient, language: Language) {
  const [draft, setDraft] = useState<SearchDraft>(initialDraft);
  const [resource, setResource] = useState<Resource<ProductSearchData>>({ status: "loading" });
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [priceError, setPriceError] = useState<TranslationKey>();
  const [activeQuery, setActiveQuery] = useState<ProductSearchQuery>();
  const request = useRef<AbortController | undefined>(undefined);
  const lastCursor = useRef<string | undefined>(undefined);

  useEffect(() => {
    const q = draft.q.trim();
    if (q.length < 2) {
      // Suggestions belong to the previous query and must disappear before a new request.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSuggestions([]);
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      suggestProducts(client, q, controller.signal).then(
        setSuggestions,
        () => { if (!controller.signal.aborted) setSuggestions([]); }
      );
    }, 250);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [client, draft.q, language]);

  const submit = useCallback((nextDraft: SearchDraft = draft) => {
    const q = nextDraft.q.trim();
    if (!q) {
      setPriceError("mall.searchRequired");
      return;
    }
    const range = validatePriceRange(nextDraft.minPrice, nextDraft.maxPrice);
    if ("messageKey" in range) {
      setPriceError(range.messageKey);
      return;
    }
    setPriceError(undefined);
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    lastCursor.current = undefined;
    const query: ProductSearchQuery = {
      q,
      category: nextDraft.category,
      minPriceE4: range.minPriceE4,
      maxPriceE4: range.maxPriceE4,
      sort: nextDraft.sort
    };
    setDraft(nextDraft);
    setActiveQuery(query);
    setSuggestions([]);
    setResource({ status: "loading" });
    searchProducts(client, query, controller.signal).then(
      (value) => {
        if (!controller.signal.aborted) setResource({ status: "ready", value });
      },
      (error: unknown) => {
        if (!controller.signal.aborted) setResource({ status: "error", messageKey: errorMessageKey(error) });
      }
    );
  }, [client, draft]);

  const loadMore = useCallback(() => {
    if (resource.status !== "ready" || !activeQuery?.q || !resource.value.next_cursor) return;
    const cursor = resource.value.next_cursor;
    if (lastCursor.current === cursor) return;
    lastCursor.current = cursor;
    const controller = new AbortController();
    request.current?.abort();
    request.current = controller;
    searchProducts(client, { ...activeQuery, cursor }, controller.signal).then(
      (page) => {
        if (controller.signal.aborted) return;
        const nextCursor = page.next_cursor && page.next_cursor !== cursor && page.items.length > 0
          ? page.next_cursor
          : null;
        setResource({
          status: "ready",
          value: {
            ...page,
            next_cursor: nextCursor,
            items: [...resource.value.items, ...page.items]
          }
        });
      },
      (error: unknown) => {
        if (!controller.signal.aborted) setResource({ status: "error", messageKey: errorMessageKey(error), previous: resource.status === "ready" ? resource.value : undefined });
      }
    );
  }, [activeQuery, client, resource]);

  const retry = useCallback(() => submit(draft), [draft, submit]);
  return { draft, setDraft, resource, suggestions, priceError, submit, retry, loadMore };
}

export function SearchView({
  t,
  language,
  categories,
  actions,
  search,
  onSelectProduct,
  onBack
}: {
  categories: readonly Category[];
  actions?: ReactNode;
  search: ReturnType<typeof useProductSearch>;
  onSelectProduct: (product: ProductSearchData["items"][number]) => void;
  onBack: () => void;
} & LocaleProps) {
  const { draft, setDraft, resource, suggestions, priceError, submit, retry, loadMore } = search;
  const update = (patch: Partial<SearchDraft>) => setDraft((current) => ({ ...current, ...patch }));
  const reason = resource.status === "ready" && resource.value.reason
    ? t(resource.value.reason === "recent_in_category" ? "mall.searchReasonCategory" : "mall.searchReasonAll")
    : undefined;
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("mall.categories")} actions={actions} />
      <main className="buyer-page__body">
        <button type="button" className="buyer-back" onClick={onBack}>{t("common.back")}</button>
        <Card>
          <form className="buyer-search-form" role="search" onSubmit={(event) => { event.preventDefault(); submit(); }}>
            <label>
              <span>{t("mall.searchPlaceholder")}</span>
              <input value={draft.q} onChange={(event) => update({ q: event.target.value })} />
            </label>
            {suggestions.length > 0 && (
              <div className="buyer-search-suggestions" aria-label={t("mall.searchSuggest")}>
                {suggestions.map((suggestion) => (
                  <button key={suggestion} type="button" onClick={() => submit({ ...draft, q: suggestion })}>{suggestion}</button>
                ))}
              </div>
            )}
            <label><span>{t("mall.searchCategory")}</span><select value={draft.category ?? ""} onChange={(event) => update({ category: event.target.value || undefined })}><option value="">{t("mall.searchAllCategories")}</option>{categories.map((category) => <option key={category.public_id} value={category.code}>{localized(category.name, language)}</option>)}</select></label>
            <div className="buyer-search-form__prices">
              <label><span>{t("mall.searchMinPrice")}</span><input inputMode="decimal" value={draft.minPrice} onChange={(event) => update({ minPrice: event.target.value })} /></label>
              <label><span>{t("mall.searchMaxPrice")}</span><input inputMode="decimal" value={draft.maxPrice} onChange={(event) => update({ maxPrice: event.target.value })} /></label>
            </div>
            <label><span>{t("mall.searchSort")}</span><select value={draft.sort} onChange={(event) => update({ sort: event.target.value as ProductSearchSort })}><option value="relevance">{t("mall.searchSortRelevance")}</option><option value="price_asc">{t("mall.searchSortPriceAsc")}</option><option value="price_desc">{t("mall.searchSortPriceDesc")}</option><option value="newest">{t("mall.searchSortNewest")}</option></select></label>
            {priceError && <p className="buyer-search-error" role="alert">{t(priceError)}</p>}
            <button type="submit" className="buyer-search-submit">{t("mall.searchSubmit")}</button>
          </form>
        </Card>
        {resource.status === "loading" && <p role="status">{t("common.loading")}</p>}
        {resource.status === "error" && <Card><p className="buyer-search-error" role="alert">{t(resource.messageKey)}</p><button type="button" onClick={retry}>{t("common.retry")}</button>{resource.previous && resource.previous.items.length > 0 && <ProductList products={resource.previous.items} t={t} language={language} onSelect={onSelectProduct} />}</Card>}
        {resource.status === "ready" && resource.value.items.length === 0 && (
          <Card>
            <h2 className="buyer-section__title">{t("mall.searchNoResults")}</h2>
            {reason && <p>{reason}</p>}
            {resource.value.recommendations.length > 0 && <><h3>{t("mall.searchRecommendations")}</h3><ProductList products={resource.value.recommendations} t={t} language={language} onSelect={onSelectProduct} /></>}
          </Card>
        )}
        {resource.status === "ready" && resource.value.items.length > 0 && <Card><ProductList products={resource.value.items} t={t} language={language} onSelect={onSelectProduct} />{resource.value.next_cursor && <button type="button" className="buyer-load-more" onClick={loadMore}>{t("mall.loadMore")}</button>}</Card>}
      </main>
    </div>
  );
}
