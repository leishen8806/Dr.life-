import { StatusTag } from "@drlife/ui";
import { localized } from "../localized";
import { entryTier, formatPriceE4 } from "../money";
import type { Product } from "../types";
import type { LocaleProps } from "./common";

export function originLabel(
  product: Product,
  t: LocaleProps["t"]
): string {
  return t(product.origin === "cn" ? "mall.origin.cn" : "mall.origin.lc");
}

export function OriginTag({ product, t }: { product: Product } & Pick<LocaleProps, "t">) {
  return (
    <StatusTag tone={product.origin === "cn" ? "neutral" : "success"}>
      {originLabel(product, t)}
    </StatusTag>
  );
}

/** One row of the catalogue: name, spec, origin, minimum quantity and price. */
export function ProductRow({
  product,
  t,
  language,
  onSelect
}: {
  product: Product;
  onSelect: (product: Product) => void;
} & LocaleProps) {
  const tier = entryTier(product.tiers);
  return (
    <button
      type="button"
      className="buyer-product"
      onClick={() => onSelect(product)}
    >
      <span className="buyer-product__name" lang={language}>
        {localized(product.name, language)}
      </span>
      {product.spec && (
        <span className="buyer-product__spec" lang={language}>
          {localized(product.spec, language)}
        </span>
      )}
      <span className="buyer-product__meta">
        <OriginTag product={product} t={t} />
        {tier && (
          <span className="buyer-product__fact">
            <small>{t("mall.moq")}</small>
            <span>
              {tier.min_qty} {product.unit}
            </span>
          </span>
        )}
        {typeof product.lead_days === "number" && (
          <span className="buyer-product__fact">
            <small>{t("mall.leadTime")}</small>
            <span>{t("mall.leadDays", { days: product.lead_days })}</span>
          </span>
        )}
      </span>
      {tier && (
        <span className="buyer-product__price">
          {t("mall.priceFromValue", { price: formatPriceE4(tier.unit_price_e4) })}
          <small> / {product.unit}</small>
        </span>
      )}
    </button>
  );
}

export function ProductList({
  products,
  t,
  language,
  onSelect
}: {
  products: readonly Product[];
  onSelect: (product: Product) => void;
} & LocaleProps) {
  return (
    <div className="buyer-product-list">
      {products.map((product) => (
        <ProductRow
          key={product.public_id}
          product={product}
          t={t}
          language={language}
          onSelect={onSelect}
        />
      ))}
    </div>
  );
}
