import type { ReactNode } from "react";
import { AppHeader, Card } from "@drlife/ui";
import { localized } from "../localized";
import { formatPriceE4 } from "../money";
import type { BuyerOrder } from "../orders";
import type { Resource } from "../resource";
import type { LocaleProps } from "./common";

export function OrderDetailView({
  t,
  language,
  order,
  actions,
  onBack
}: { order: Resource<BuyerOrder>; actions?: ReactNode; onBack: () => void } & LocaleProps) {
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("order.detailTitle")} actions={actions} />
      <main className="buyer-page__body">
        <button type="button" className="buyer-back" onClick={onBack}>{t("common.back")}</button>
        {order.status === "loading" && <Card><p>{t("common.loading")}</p></Card>}
        {order.status === "error" && <Card><p className="buyer-order-warning">{t(order.messageKey)}</p></Card>}
        {order.status === "ready" && <>
        <Card>
          <div className="buyer-order-detail__heading">
            <h1 className="buyer-section__title">{order.value.publicId}</h1>
            <span className="buyer-order-status">{t("order.parentSummary")}</span>
          </div>
          <p className="buyer-order-detail__meta">{t("order.createdAt")}: {order.value.createdAt.slice(0, 10)}</p>
        </Card>
        {order.value.subOrders.map((subOrder) => (
          <Card key={subOrder.publicId}>
            <div className="buyer-order-detail__heading">
              <h2 className="buyer-section__title">{localized(subOrder.merchantName, language) || t("order.merchantFallback")}</h2>
              <span className="buyer-order-status">{t(`order.buyerStatus.${subOrder.status}` as const)}</span>
            </div>
            <p className="buyer-order-detail__meta">{t("order.subOrder")}: {subOrder.publicId}</p>
            <p className="buyer-order-detail__meta">{t("order.paymentStatus")}: {t(`order.payment.${order.value.paymentStatus}` as const)}</p>
            {order.value.paymentStatus === "confirming" && <p className="buyer-order-warning">{t("order.paymentConfirming")}</p>}
            <h3 className="buyer-section__title">{t("order.itemsTitle")}</h3>
            <div className="buyer-order-lines">
              {subOrder.lines.map((line, lineIndex) => (
                <div className="buyer-order-line" key={line.publicId}>
                  <div>
                    <strong lang={language}>{localized(line.name, language) || t("order.itemFallback", { count: lineIndex + 1 })}</strong>
                    <small>{line.quantity} {line.unit} × {formatPriceE4(line.unitPriceE4)}</small>
                  </div>
                  <strong>{formatPriceE4(line.totalE4)}</strong>
                </div>
              ))}
            </div>
            <p className="buyer-order-total"><span>{t("order.goods")}</span><strong>{formatPriceE4(subOrder.goodsAmountE4)}</strong></p>
            <p className="buyer-order-detail__meta">{t("order.shipping")}: {subOrder.shippingAmountE4 === 0n ? t("order.free") : formatPriceE4(subOrder.shippingAmountE4)}</p>
            <p className="buyer-order-total"><span>{t("order.total")}</span><strong>{formatPriceE4(subOrder.totalE4)}</strong></p>
            {subOrder.shippingQuoteExpiresAt && <p className="buyer-order-detail__meta">{t("order.quoteExpires")}: {subOrder.shippingQuoteExpiresAt.slice(0, 10)}</p>}
            <h3 className="buyer-section__title">{t("order.delivery")}</h3>
            <ol className="buyer-order-events">
              {subOrder.events.map((event) => (
                <li key={`${event.label}-${event.at}`}>
                  <strong>{t(`order.buyerStatus.${event.label}` as const)}</strong>
                  <small>{event.at.slice(0, 10)}</small>
                </li>
              ))}
            </ol>
          </Card>
        ))}
        <Card>
          <p className="buyer-order-total"><span>{t("order.goods")}</span><strong>{formatPriceE4(order.value.goodsAmountE4)}</strong></p>
          <p className="buyer-order-total"><span>{t("order.shipping")}</span><strong>{order.value.shippingAmountE4 === 0n ? t("order.free") : formatPriceE4(order.value.shippingAmountE4)}</strong></p>
          <p className="buyer-order-total"><span>{t("order.total")}</span><strong>{formatPriceE4(order.value.totalE4)}</strong></p>
        </Card>
        </>}
      </main>
    </div>
  );
}
