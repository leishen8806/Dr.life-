import type { ReactNode } from "react";
import { AppHeader, EmptyState, ErrorState, Loading } from "@drlife/ui";
import { localized } from "../localized";
import { formatPriceE4 } from "../money";
import type { Resource } from "../resource";
import type { BuyerOrder, BuyerSubOrder, OrderStatus } from "../orders";
import type { LocaleProps } from "./common";
import type { TranslationKey } from "@drlife/i18n";

function statusKey(status: OrderStatus) {
  return `order.buyerStatus.${status}` as const;
}

function statusSummary(subOrders: readonly BuyerSubOrder[], t: LocaleProps["t"]) {
  return subOrders.map((item) => t(statusKey(item.status))).join(" · ");
}

export function OrderListView({
  t,
  language,
  orders,
  actions,
  onBack,
  onRetry,
  onOpen,
  onLoadMore,
  loadingMore,
  moreMessageKey,
}: {
  orders: Resource<readonly BuyerOrder[]>;
  actions?: ReactNode;
  onBack: () => void;
  onRetry: () => void;
  onOpen: (order: BuyerOrder) => void;
  onLoadMore?: () => void;
  loadingMore?: boolean;
  moreMessageKey?: TranslationKey;
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
                  <span className="buyer-order-status">{statusSummary(item.subOrders, t)}</span>
                </span>
                <span className="buyer-order-card__merchant">
                  {item.subOrders.map((subOrder) => localized(subOrder.merchantName, language) || t("order.merchantFallback")).join(" · ")}
                </span>
                <span className="buyer-order-card__summary">
                  {t("order.subOrderCount", { count: item.subOrders.length })} · {t("order.items", {
                    count: item.subOrders.reduce((total, subOrder) => total + subOrder.lines.length, 0)
                  })}
                </span>
                <span className="buyer-order-card__footer">
                  <strong>{formatPriceE4(item.totalE4)}</strong>
                  <span>{t("order.viewDetails")}</span>
                </span>
              </button>
            ))}
          </section>
        )}
        {orders.status === "ready" && orders.value.length > 0 && onLoadMore && (
          <div className="buyer-order-list__more">
            {moreMessageKey && <p className="buyer-order-warning">{t(moreMessageKey)}</p>}
            <button type="button" onClick={onLoadMore} disabled={loadingMore}>
              {loadingMore ? t("common.loading") : t("order.loadMore")}
            </button>
          </div>
        )}
      </main>
    </div>
  );
}
