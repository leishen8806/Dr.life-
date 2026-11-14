import { useEffect, useMemo, useState, type ReactNode } from "react";
import { AppHeader, Button, EmptyState, ErrorState, Loading } from "@drlife/ui";
import { localized } from "../localized";
import { formatPriceE4 } from "../money";
import type { Resource } from "../resource";
import type { BuyerOrder, BuyerSubOrder, OrderStatus } from "../orders";
import type { LocaleProps } from "./common";
import type { TranslationKey } from "@drlife/i18n";
import { formatOrderDateTime } from "../order-date";
import { isOrderQuotePending } from "../order-quote";

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
  const [now, setNow] = useState(() => Date.now());
  const nextExpiry = useMemo(() => orders.status !== "ready" ? undefined : orders.value
      .flatMap((order) => order.subOrders)
      .filter((subOrder) => subOrder.status === "awaiting_payment")
      .map((subOrder) => Date.parse(subOrder.shippingQuoteExpiresAt))
      .filter((value) => Number.isFinite(value) && value > now)
      .sort((a, b) => a - b)[0], [orders]);

  useEffect(() => {
    if (nextExpiry === undefined || nextExpiry <= now) return;
    const timer = window.setTimeout(() => setNow(Date.now()), Math.min(nextExpiry - now, 2_147_483_647));
    return () => window.clearTimeout(timer);
  }, [nextExpiry, now]);
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("order.pageTitle")} actions={actions} />
      <main className="buyer-page__body">
        <Button variant="text" className="buyer-back" onClick={onBack}>{t("common.back")}</Button>
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
            {orders.value.map((item) => item.dataError ? (
              <div className="buyer-order-card buyer-order-card--error" key={`data-error-${item.dataErrorPublicId ?? item.publicId}`} role="alert">
                <strong>{t("error.1001")}</strong>
              </div>
            ) : (
              <Button variant="text" className="buyer-order-card" key={item.publicId} onClick={() => onOpen(item)}>
                <span className="buyer-order-card__header">
                  <strong>{t("order.detailTitle")}</strong>
                  <span className="buyer-order-status">{statusSummary(item.subOrders, t)}</span>
                </span>
                <span className="buyer-order-card__merchant">
                  {item.subOrders.map((subOrder) => localized(subOrder.merchantName, language) || t("order.merchantFallback")).join(" · ")}
                </span>
                <span className="buyer-order-card__summary">
                  {t("order.subOrderCount", { count: item.subOrders.length })} · {t(item.subOrders.reduce((total, subOrder) => total + subOrder.lines.length, 0) === 1 ? "order.items.one" : "order.items.other", {
                    count: item.subOrders.reduce((total, subOrder) => total + subOrder.lines.length, 0)
                  })}
                </span>
                <span className="buyer-order-card__footer">
                  <strong>{isOrderQuotePending(item, now) ? t("order.shippingPending") : formatPriceE4(item.totalE4)}</strong>
                  <span>{formatOrderDateTime(item.createdAt, language)}</span>
                  <span>{t("order.viewDetails")}</span>
                </span>
              </Button>
            ))}
          </section>
        )}
        {orders.status === "ready" && orders.value.length > 0 && onLoadMore && (
          <div className="buyer-order-list__more">
            {moreMessageKey && <p className="buyer-order-warning">{t(moreMessageKey)}</p>}
            <Button variant="secondary" onClick={onLoadMore} loading={loadingMore}>
              {loadingMore ? t("common.loading") : t("order.loadMore")}
            </Button>
          </div>
        )}
      </main>
    </div>
  );
}
