import type { ReactNode } from "react";
import { Card, AppHeader } from "@drlife/ui";
import { localized } from "../localized";
import { cartTotalE4, lineTotalE4, lineUnitPrice, type CartError, type CartItem } from "../cart";
import { formatPriceE4 } from "../money";
import type { LocaleProps } from "./common";

function groupItems(items: readonly CartItem[]) {
  return [...new Set(items.map((item) => item.merchantId))]
    .map((merchantId) => ({
      merchantId,
      merchantName: items.find((item) => item.merchantId === merchantId)!.merchantName,
      items: items.filter((item) => item.merchantId === merchantId)
    }))
    .filter((group) => group.items.length > 0);
}

function errorLabel(t: LocaleProps["t"], error?: CartError) {
  if (error === "overAvailable") return t("mall.cartOutOfStock");
  if (error === "belowMoq") return t("mall.cartBelowMoq");
  return error === "invalidQuantity" ? t("mall.cartIntegerOnly") : undefined;
}

export function CartView({
  t,
  language,
  items,
  error,
  actions,
  onBack,
  onUpdateQuantity,
  onStepQuantity,
  onRemove
}: {
  items: readonly CartItem[];
  error?: CartError;
  actions?: ReactNode;
  onBack: () => void;
  onUpdateQuantity: (item: CartItem, quantity: string) => void;
  onStepQuantity: (item: CartItem, delta: -1 | 1) => void;
  onRemove: (item: CartItem) => void;
} & LocaleProps) {
  const groups = groupItems(items);
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("mall.cart")} actions={actions} />
      <main className="buyer-page__body">
        <button type="button" className="buyer-back" onClick={onBack}>{t("common.back")}</button>
        {items.length === 0 ? (
          <Card>
            <section className="buyer-cart-empty" aria-live="polite">
              <h1>{t("mall.cartEmpty")}</h1>
              <button type="button" className="buyer-cart-primary" onClick={onBack}>{t("mall.browseProducts")}</button>
            </section>
          </Card>
        ) : (
          <>
            <h1 className="buyer-section__title">{t("mall.cartSplitPreview")}</h1>
            {error && <p className="buyer-cart-error" role="alert">{errorLabel(t, error)}</p>}
            {groups.map((group) => {
              const subtotal = group.items.reduce((total, item) => total + lineTotalE4(item), 0n);
              return (
                <Card key={group.merchantId}>
                  <h2 className="buyer-section__title">{localized(group.merchantName, language)}</h2>
                  <div className="buyer-cart-lines">
                    {group.items.map((item) => (
                      <article className="buyer-cart-line" key={item.product.public_id}>
                        <div className="buyer-cart-line__copy" lang={language}>
                          <strong>{localized(item.product.name, language)}</strong>
                          <small>{formatPriceE4(lineUnitPrice(item))} / {item.product.unit}</small>
                          <small>{t("mall.cartAvailable", { quantity: item.availableQuantity })}</small>
                        </div>
                        <div className="buyer-cart-line__controls">
                          <button type="button" aria-label={`${t("mall.cartQuantity")} -`} onClick={() => onStepQuantity(item, -1)}>−</button>
                          <input
                            aria-label={t("mall.cartQuantity")}
                            inputMode="numeric"
                            min="0"
                            step="1"
                            type="number"
                            value={item.quantity}
                            onChange={(event) => onUpdateQuantity(item, event.target.value)}
                          />
                          <button type="button" aria-label={`${t("mall.cartQuantity")} +`} onClick={() => onStepQuantity(item, 1)}>+</button>
                        </div>
                        <div className="buyer-cart-line__total">
                          <strong>{formatPriceE4(lineTotalE4(item))}</strong>
                          <button type="button" className="buyer-cart-remove" onClick={() => onRemove(item)}>{t("mall.cartRemove")}</button>
                        </div>
                      </article>
                    ))}
                  </div>
                  <p className="buyer-cart-subtotal"><span>{t("mall.cartSubtotal")}</span><strong>{formatPriceE4(subtotal)}</strong></p>
                </Card>
              );
            })}
            <Card>
              <p className="buyer-cart-total"><span>{t("mall.cartTotal")}</span><strong>{formatPriceE4(cartTotalE4(items))}</strong></p>
              <p className="buyer-cart-note">{t("mall.cartEstimateNote")}</p>
              <button type="button" className="buyer-cart-primary" disabled>{t("mall.cartPlaceOrder")}</button>
            </Card>
          </>
        )}
      </main>
    </div>
  );
}
