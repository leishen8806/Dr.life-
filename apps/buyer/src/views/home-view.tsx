import type { ReactNode } from "react";
import { AppHeader, Button, Card } from "@drlife/ui";
import { localized } from "../localized";
import type { Resource } from "../resource";
import type { Category, Product } from "../types";
import type { LocaleProps, Translate } from "./common";
import { ProductList } from "./product-list";
import { isEmptyList, ResourceSection } from "./resource-section";

/**
 * Entry only for now: the result page lands with the search slice (T-03).
 */
export function SearchEntry({ t, onSearch }: { t: Translate; onSearch: () => void }) {
  return (
    <div className="buyer-search" role="search">
      <Button variant="text" className="buyer-search__placeholder" onClick={onSearch}>
        {t("mall.searchPlaceholder")}
      </Button>
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
        <Button
          key={category.public_id}
          variant="text"
          className="buyer-category"
          lang={language}
          onClick={() => onSelect(category.code)}
        >
          {localized(category.name, language)}
        </Button>
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
  onSelectProduct,
  onSearch
}: {
  categories: Resource<Category[]>;
  featured: Resource<Product[]>;
  actions?: ReactNode;
  onRetryCategories: () => void;
  onRetryFeatured: () => void;
  onSelectCategory: (code: string) => void;
  onSelectProduct: (product: Product) => void;
  onSearch: () => void;
} & LocaleProps) {
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("brand.slogan")} actions={actions} />
      <main className="buyer-page__body">
        <SearchEntry t={t} onSearch={onSearch} />
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
