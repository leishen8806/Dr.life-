import type { ReactNode } from "react";
import { AppHeader, Card } from "@drlife/ui";
import { localized } from "../localized";
import type { Resource } from "../resource";
import type { Category, Product } from "../types";
import type { LocaleProps, Translate } from "./common";
import { ProductList } from "./product-list";
import { isEmptyList, ResourceSection } from "./resource-section";

/**
 * Entry only for now: the result page lands with the search slice (T-03).
 */
export function SearchEntry({ t }: { t: Translate }) {
  return (
    <div className="buyer-search" role="search">
      <span className="buyer-search__placeholder">
        {t("mall.searchPlaceholder")}
      </span>
      <small className="buyer-search__note">{t("mall.searchComingSoon")}</small>
    </div>
  );
}

export function CategoryGrid({
  categories,
  language,
  onSelect
}: {
  categories: readonly Category[];
  onSelect: (code: string) => void;
} & Pick<LocaleProps, "language">) {
  return (
    <div className="buyer-category-grid">
      {categories.map((category) => (
        <button
          key={category.public_id}
          type="button"
          className="buyer-category"
          lang={language}
          onClick={() => onSelect(category.code)}
        >
          {localized(category.name, language)}
        </button>
      ))}
    </div>
  );
}

export function HomeView({
  t,
  language,
  categories,
  featured,
  actions,
  onRetryCategories,
  onRetryFeatured,
  onSelectCategory,
  onSelectProduct
}: {
  categories: Resource<Category[]>;
  featured: Resource<Product[]>;
  actions?: ReactNode;
  onRetryCategories: () => void;
  onRetryFeatured: () => void;
  onSelectCategory: (code: string) => void;
  onSelectProduct: (product: Product) => void;
} & LocaleProps) {
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("brand.slogan")} actions={actions} />
      <main className="buyer-page__body">
        <SearchEntry t={t} />
        <Card>
          <h2 className="buyer-section__title">{t("mall.categories")}</h2>
          <ResourceSection
            resource={categories}
            t={t}
            emptyKey="mall.categoriesEmpty"
            isEmpty={isEmptyList}
            onRetry={onRetryCategories}
          >
            {(items) => (
              <CategoryGrid
                categories={items}
                language={language}
                onSelect={onSelectCategory}
              />
            )}
          </ResourceSection>
        </Card>
        <Card>
          <h2 className="buyer-section__title">{t("mall.featured")}</h2>
          <ResourceSection
            resource={featured}
            t={t}
            emptyKey="mall.productsEmpty"
            isEmpty={isEmptyList}
            onRetry={onRetryFeatured}
          >
            {(items) => (
              <ProductList
                products={items}
                t={t}
                language={language}
                onSelect={onSelectProduct}
              />
            )}
          </ResourceSection>
        </Card>
      </main>
    </div>
  );
}
