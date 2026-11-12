import type { ReactNode } from "react";
import { AppHeader, Button, Card } from "@drlife/ui";
import { plural, type TranslationKey } from "@drlife/i18n";
import type { ProductFeed } from "../feed";
import { localized } from "../localized";
import type { Resource } from "../resource";
import type { Category, Product } from "../types";
import type { LocaleProps, Translate } from "./common";
import { ProductList } from "./product-list";
import { ResourceSection } from "./resource-section";

function CategoryRail({
  categories,
  activeCode,
  language,
  allLabel,
  onSelect
}: {
  categories: readonly Category[];
  activeCode?: string;
  allLabel: string;
  onSelect: (code?: string) => void;
} & Pick<LocaleProps, "language">) {
  return (
    <nav className="buyer-category-rail" aria-label={allLabel}>
      <Button
        variant="text"
        ariaPressed={activeCode === undefined}
        className="buyer-category-rail__item"
        onClick={() => onSelect(undefined)}
      >
        {allLabel}
      </Button>
      {categories.map((category) => (
        <Button
          key={category.public_id}
          variant="text"
          className="buyer-category-rail__item"
          lang={language}
          ariaPressed={category.code === activeCode}
          onClick={() => onSelect(category.code)}
        >
          {localized(category.name, language)}
        </Button>
      ))}
    </nav>
  );
}

/** Reaches the pages after the first; a failed page keeps the rows on screen. */
function LoadMore({
  t,
  feed,
  loadingMore,
  moreMessageKey,
  onLoadMore
}: {
  t: Translate;
  feed: ProductFeed;
  loadingMore: boolean;
  moreMessageKey?: TranslationKey;
  onLoadMore: () => void;
}) {
  if (!feed.hasMore) return null;
  return (
    <div className="buyer-load-more-row">
      {moreMessageKey && (
        <p className="buyer-load-more__error" role="alert">
          {t(moreMessageKey)}
        </p>
      )}
      <Button
        variant="text"
        className="buyer-load-more"
        loading={loadingMore}
        onClick={onLoadMore}
      >
        {t(loadingMore ? "common.loading" : "mall.loadMore")}
      </Button>
    </div>
  );
}

export function CategoryView({
  t,
  language,
  categories,
  activeCode,
  products,
  loadingMore,
  moreMessageKey,
  actions,
  onSelectCategory,
  onSelectProduct,
  onRetry,
  onLoadMore,
  onBack
}: {
  categories: readonly Category[];
  activeCode?: string;
  products: Resource<ProductFeed>;
  loadingMore: boolean;
  moreMessageKey?: TranslationKey;
  actions?: ReactNode;
  onSelectCategory: (code?: string) => void;
  onSelectProduct: (product: Product) => void;
  onRetry: () => void;
  onLoadMore: () => void;
  onBack: () => void;
} & LocaleProps) {
  const active = categories.find((category) => category.code === activeCode);
  const title = active
    ? localized(active.name, language)
    : t("mall.allCategories");
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("mall.categories")} actions={actions} />
      <main className="buyer-page__body">
        <Button variant="text" className="buyer-back" onClick={onBack}>
          {t("common.back")}
        </Button>
        <div className="buyer-mall">
          <CategoryRail
            categories={categories}
            activeCode={activeCode}
            language={language}
            allLabel={t("mall.allCategories")}
            onSelect={onSelectCategory}
          />
          <Card>
            <header className="buyer-list-header">
              <h2 className="buyer-section__title" lang={active ? language : undefined}>
                {title}
              </h2>
              {products.status === "ready" && products.value.total > 0 && (
                <small>
                  {t(
                    plural(
                      products.value.total,
                      "mall.productCount.one",
                      "mall.productCount.other"
                    ),
                    { count: products.value.total }
                  )}
                </small>
              )}
            </header>
            <ResourceSection
              resource={products}
              t={t}
              emptyKey="mall.productsEmpty"
              isEmpty={(feed) => feed.items.length === 0}
              onRetry={onRetry}
            >
              {(feed) => (
                <>
                  <ProductList
                    products={feed.items}
                    t={t}
                    language={language}
                    onSelect={onSelectProduct}
                  />
                  <LoadMore
                    t={t}
                    feed={feed}
                    loadingMore={loadingMore}
                    moreMessageKey={moreMessageKey}
                    onLoadMore={onLoadMore}
                  />
                </>
              )}
            </ResourceSection>
          </Card>
        </div>
      </main>
    </div>
  );
}
