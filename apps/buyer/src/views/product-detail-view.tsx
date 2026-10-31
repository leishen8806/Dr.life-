import type { ReactNode } from "react";
import { AppHeader, Card } from "@drlife/ui";
import { localized } from "../localized";
import { entryTier, formatPriceE4, sortedTiers } from "../money";
import type { Product } from "../types";
import type { LocaleProps } from "./common";
import { OriginTag } from "./product-list";

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="buyer-fact">
      <span className="buyer-fact__label">{label}</span>
      <span className="buyer-fact__value">{children}</span>
    </div>
  );
}

/**
 * The public catalogue has no product detail endpoint yet, so the product comes
 * from the list the buyer tapped. `merchantName` stays optional until the API
 * exposes the merchant behind a product.
 */
export function ProductDetailView({
  t,
  language,
  product,
  merchantName,
  actions,
  onBack
}: {
  product: Product;
  merchantName?: string;
  actions?: ReactNode;
  onBack: () => void;
} & LocaleProps) {
  const tiers = sortedTiers(product.tiers);
  const entry = entryTier(product.tiers);
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("mall.detail")} actions={actions} />
      <main className="buyer-page__body">
        <button type="button" className="buyer-back" onClick={onBack}>
          {t("common.back")}
        </button>
        <Card>
          <OriginTag product={product} t={t} />
          <h1 className="buyer-detail__name" lang={language}>
            {localized(product.name, language)}
          </h1>
          {product.spec && (
            <p className="buyer-detail__spec" lang={language}>
              {localized(product.spec, language)}
            </p>
          )}
          {entry && (
            <p className="buyer-detail__price">
              {t("mall.priceFromValue", {
                price: formatPriceE4(entry.unit_price_e4)
              })}
              <small> / {product.unit}</small>
            </p>
          )}
        </Card>
        <Card>
          <h2 className="buyer-section__title">{t("mall.tiers")}</h2>
          <div className="buyer-tiers">
            {tiers.map((tier) => (
              <div className="buyer-tier" key={tier.min_qty}>
                <span className="buyer-tier__quantity">
                  {t("mall.tierQuantity", { quantity: tier.min_qty })}
                </span>
                <span className="buyer-tier__price">
                  {formatPriceE4(tier.unit_price_e4)}
                </span>
              </div>
            ))}
          </div>
        </Card>
        <Card>
          <Fact label={t("mall.unit")}>{product.unit}</Fact>
          {entry && (
            <Fact label={t("mall.moq")}>
              {entry.min_qty} {product.unit}
            </Fact>
          )}
          {typeof product.lead_days === "number" && (
            <Fact label={t("mall.leadTime")}>
              {t("mall.leadDays", { days: product.lead_days })}
            </Fact>
          )}
          {merchantName && (
            <Fact label={t("mall.supplier")}>{merchantName}</Fact>
          )}
        </Card>
      </main>
    </div>
  );
}
