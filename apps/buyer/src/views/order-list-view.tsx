import type { ReactNode } from "react";
import { AppHeader, EmptyState, ErrorState, Loading } from "@drlife/ui";
import { localized } from "../localized";
import { formatPriceE4 } from "../money";
import type { Resource } from "../resource";
import type { BuyerOrder, OrderStatus } from "../orders";
import type { LocaleProps } from "./common";

function statusKey(status: OrderStatus) {
  return `order.status.${status}` as const;
}

export function OrderListView({
  t,
  language,
  orders,
  actions,
  onBack,
  onRetry,
  onOpen
}: {
  orders: Resource<readonly BuyerOrder[]>;
  actions?: ReactNode;
  onBack: () => void;
  onRetry: () => void;
  onOpen: (order: BuyerOrder) => void;
} & LocaleProps) {
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("order.pageTitle")} actions={actions} />
      <main className="buyer-page__body">
        <button type="button" className="buyer-back" onClick={onBack}>{t("common.back")}</button>
        {orders.status === "loading" && <Loading label={t("common.loading")} skeleton />}
        {orders.status === "error" && (
          <ErrorState
            title={t("order.listError")}
            description={t(orders.messageKey)}
            retryLabel={t("common.retry")}
            onRetry={onRetry}
          />
        )}
        {orders.status === "ready" && orders.value.length === 0 && (
          <EmptyState title={t("order.listEmpty")} actionLabel={t("common.refresh")} onAction={onRetry} />
        )}
        {orders.status === "ready" && orders.value.length > 0 && (
          <section className="buyer-order-list" aria-label={t("order.pageTitle")}>
            {orders.value.map((item) => (
              <button type="button" className="buyer-order-card" key={item.publicId} onClick={() => onOpen(item)}>
                <span className="buyer-order-card__header">
                  <strong>{item.publicId}</strong>
                  <span className="buyer-order-status">{t(statusKey(item.status))}</span>
                </span>
                <span className="buyer-order-card__merchant">{localized(item.merchantName, language)}</span>
                <span className="buyer-order-card__summary">
                  {localized(item.lines[0]?.name, language)} · {t("order.items", { count: item.lines.length })}
                </span>
                <span className="buyer-order-card__footer">
                  <strong>{formatPriceE4(item.totalE4)}</strong>
                  <span>{t("order.viewDetails")}</span>
                </span>
              </button>
            ))}
          </section>
        )}
        {orders.status === "ready" && orders.value.length > 0 && (
          <p className="buyer-order-note">{t("order.mockNote")}</p>
        )}
      </main>
    </div>
  );
}
