import type { ReactNode } from "react";
import { AppHeader, Card } from "@drlife/ui";
import { plural } from "@drlife/i18n";
import { localized } from "../localized";
import type { Resource } from "../resource";
import type { Category, Product } from "../types";
import type { LocaleProps } from "./common";
import { ProductList } from "./product-list";
import { isEmptyList, ResourceSection } from "./resource-section";

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
      <button
        type="button"
        className="buyer-category-rail__item"
        aria-pressed={activeCode === undefined}
        onClick={() => onSelect(undefined)}
      >
        {allLabel}
      </button>
      {categories.map((category) => (
        <button
          key={category.public_id}
          type="button"
          className="buyer-category-rail__item"
          lang={language}
          aria-pressed={category.code === activeCode}
          onClick={() => onSelect(category.code)}
        >
          {localized(category.name, language)}
        </button>
      ))}
    </nav>
  );
}

export function CategoryView({
  t,
  language,
  categories,
  activeCode,
  products,
  actions,
  onSelectCategory,
  onSelectProduct,
  onRetry,
  onBack
}: {
  categories: readonly Category[];
  activeCode?: string;
  products: Resource<Product[]>;
  actions?: ReactNode;
  onSelectCategory: (code?: string) => void;
  onSelectProduct: (product: Product) => void;
  onRetry: () => void;
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
        <button type="button" className="buyer-back" onClick={onBack}>
          {t("common.back")}
        </button>
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
              {products.status === "ready" && (
                <small>
                  {t(
                    plural(
                      products.value.length,
                      "mall.productCount.one",
                      "mall.productCount.other"
                    ),
                    { count: products.value.length }
                  )}
                </small>
              )}
            </header>
            <ResourceSection
              resource={products}
              t={t}
              emptyKey="mall.productsEmpty"
              isEmpty={isEmptyList}
              onRetry={onRetry}
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
        </div>
      </main>
    </div>
  );
}
