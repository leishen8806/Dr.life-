import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { AppHeader, Button, Card } from "@drlife/ui";
import { localized } from "../localized";
import { formatPriceE4 } from "../money";
import type { BuyerOrder } from "../orders";
import type { Resource } from "../resource";
import { canPay } from "../payment";
import type { LocaleProps } from "./common";
import { formatOrderDateTime } from "../order-date";
import { isOrderQuotePending, isShippingQuotePending } from "../order-quote";

function formatQuoteExpiry(value: string, language: LocaleProps["language"]): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(language, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "Asia/Phnom_Penh"
  }).format(date);
}

function isExpiredQuote(value: string, now: number): boolean {
  const expiry = Date.parse(value);
  return Number.isFinite(expiry) && expiry <= now;
}

export function OrderDetailView({
  t,
  language,
  order,
  actions,
  onRetry,
  onBack,
  onPay
}: { order: Resource<BuyerOrder>; actions?: ReactNode; onRetry?: () => void; onBack: () => void; onPay?: () => void } & LocaleProps) {
  const [now, setNow] = useState(() => Date.now());
  const expiry = useMemo(() => order.status !== "ready" ? undefined : order.value.subOrders
      .map((subOrder) => Date.parse(subOrder.shippingQuoteExpiresAt))
      .filter((value) => Number.isFinite(value) && value > now)
      .sort((a, b) => a - b)[0], [order]);

  useEffect(() => {
    if (expiry === undefined || expiry <= now) return;
    const delay = Math.min(expiry - now, 2_147_483_647);
    const timer = window.setTimeout(() => setNow(Date.now()), delay);
    return () => window.clearTimeout(timer);
  }, [expiry, now]);

  // Client time only controls display/DEV mock gating; real payment must use
  // the server's 409/3006 response before creating a payment intent.
  const paymentQuoteUnavailable = order.status === "ready" && isOrderQuotePending(order.value, now);

  return (
    <div className="buyer-page">
      <AppHeader slogan={t("order.detailTitle")} actions={actions} />
      <main className="buyer-page__body">
        <Button variant="text" className="buyer-back" onClick={onBack}>{t("common.back")}</Button>
        {order.status === "loading" && <Card><p>{t("common.loading")}</p></Card>}
        {order.status === "error" && <Card><p className="buyer-order-warning">{t(order.messageKey)}</p>{onRetry && <Button variant="secondary" onClick={onRetry}>{t("common.retry")}</Button>}</Card>}
        {order.status === "ready" && <>
        <Card>
          <div className="buyer-order-detail__heading">
            <h1 className="buyer-section__title">{t("order.detailTitle")}</h1>
            <span className="buyer-order-status">{t("order.parentSummary")}</span>
          </div>
          <p className="buyer-order-detail__meta">{t("order.createdAt")}: {formatOrderDateTime(order.value.createdAt, language)}</p>
        </Card>
        {order.value.subOrders.map((subOrder) => (
          <Card key={subOrder.publicId}>
            <div className="buyer-order-detail__heading">
              <h2 className="buyer-section__title">{localized(subOrder.merchantName, language) || t("order.merchantFallback")}</h2>
              <span className="buyer-order-status">{t(`order.buyerStatus.${subOrder.status}` as const)}</span>
            </div>
            <p className="buyer-order-detail__meta">{t("order.subOrder")}</p>
            <p className="buyer-order-detail__meta">{t("order.paymentStatus")}: {t(`order.payment.${order.value.paymentStatus}` as const)}</p>
            {order.value.paymentStatus === "confirming" && <p className="buyer-order-warning">{t("order.paymentConfirming")}</p>}
            <h3 className="buyer-section__title">{t("order.itemsTitle")}</h3>
            <div className="buyer-order-lines">
              {subOrder.lines.map((line, lineIndex) => (
                <div className="buyer-order-line" key={line.publicId}>
                  <div>
                    <strong lang={language}>{localized(line.name, language) || t("order.itemFallback", { count: lineIndex + 1 })}</strong>
                    <small>{line.quantity} {line.unit === "item" ? t("order.unit.item") : line.unit} × {formatPriceE4(line.unitPriceE4)}</small>
                  </div>
                  <strong>{formatPriceE4(line.totalE4)}</strong>
                </div>
              ))}
            </div>
            <p className="buyer-order-total"><span>{t("order.goods")}</span><strong>{formatPriceE4(subOrder.goodsAmountE4)}</strong></p>
            <p className="buyer-order-detail__meta">{t("order.shipping")}: {isShippingQuotePending(subOrder, now)
              ? t("order.shippingPending")
              : formatPriceE4(subOrder.shippingAmountE4)}</p>
            <p className="buyer-order-total"><span>{t("order.total")}</span><strong>{formatPriceE4(subOrder.totalE4)}</strong></p>
            {subOrder.shippingQuoteExpiresAt && subOrder.status === "awaiting_payment" && (
              <p className="buyer-order-detail__meta">
                {/* Payment follow-up owns server-authoritative 409/3006 handling; this is display-only. */}
                {isExpiredQuote(subOrder.shippingQuoteExpiresAt, now)
                  ? t("order.quoteExpired")
                  : t("order.quoteExpires")}: {formatQuoteExpiry(subOrder.shippingQuoteExpiresAt, language)}
              </p>
            )}
            {subOrder.events.length > 0 && <>
              <h3 className="buyer-section__title">{t("order.delivery")}</h3>
              <ol className="buyer-order-events">
                {subOrder.events.map((event) => (
                  <li key={`${event.label}-${event.at}`}>
                    <strong>{t(`order.buyerStatus.${event.label}` as const)}</strong>
                    <small>{event.at.slice(0, 10)}</small>
                  </li>
                ))}
              </ol>
            </>}
          </Card>
        ))}
        <Card>
          <p className="buyer-order-total"><span>{t("order.goods")}</span><strong>{formatPriceE4(order.value.goodsAmountE4)}</strong></p>
          <p className="buyer-order-total"><span>{t("order.shipping")}</span><strong>{paymentQuoteUnavailable
            ? t("order.shippingPending")
            : formatPriceE4(order.value.shippingAmountE4)}</strong></p>
          <p className="buyer-order-total"><span>{t("order.total")}</span><strong>{paymentQuoteUnavailable ? t("order.shippingPending") : formatPriceE4(order.value.totalE4)}</strong></p>
          {canPay(order.value) && !paymentQuoteUnavailable && onPay && <Button className="buyer-cart-primary" onClick={onPay}>{t("payment.payNow")}</Button>}
        </Card>
        </>}
      </main>
    </div>
  );
}
