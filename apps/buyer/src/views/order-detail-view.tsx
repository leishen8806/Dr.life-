import type { ReactNode } from "react";
import { AppHeader, Card } from "@drlife/ui";
import { localized } from "../localized";
import { formatPriceE4 } from "../money";
import type { BuyerOrder } from "../orders";
import type { LocaleProps } from "./common";

export function OrderDetailView({
  t,
  language,
  order,
  actions,
  onBack
}: { order: BuyerOrder; actions?: ReactNode; onBack: () => void } & LocaleProps) {
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("order.detailTitle")} actions={actions} />
      <main className="buyer-page__body">
        <button type="button" className="buyer-back" onClick={onBack}>{t("common.back")}</button>
        <Card>
          <div className="buyer-order-detail__heading">
            <h1 className="buyer-section__title">{order.publicId}</h1>
            <span className="buyer-order-status">{t(`order.status.${order.status}` as const)}</span>
          </div>
          <p className="buyer-order-detail__meta">{t("order.merchant")}: {localized(order.merchantName, language)}</p>
          <p className="buyer-order-detail__meta">{t("order.createdAt")}: {order.createdAt.slice(0, 10)}</p>
        </Card>
        <Card>
          <h2 className="buyer-section__title">{t("order.itemsTitle")}</h2>
          <div className="buyer-order-lines">
            {order.lines.map((line) => (
              <div className="buyer-order-line" key={line.publicId}>
                <div>
                  <strong lang={language}>{localized(line.name, language)}</strong>
                  <small>{line.quantity} {line.unit} × {formatPriceE4(line.unitPriceE4)}</small>
                </div>
                <strong>{formatPriceE4(line.totalE4)}</strong>
              </div>
            ))}
          </div>
          <p className="buyer-order-total"><span>{t("order.total")}</span><strong>{formatPriceE4(order.totalE4)}</strong></p>
        </Card>
        <Card>
          <h2 className="buyer-section__title">{t("order.delivery")}</h2>
          <ol className="buyer-order-events">
            {order.events.map((event) => (
              <li key={`${event.label}-${event.at}`}>
                <strong>{t(`order.status.${event.label}` as const)}</strong>
                <small>{event.at.slice(0, 10)}</small>
              </li>
            ))}
          </ol>
        </Card>
        <p className="buyer-order-note">{t("order.mockNote")}</p>
      </main>
    </div>
  );
}
