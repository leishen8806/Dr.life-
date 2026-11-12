import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { AppHeader, Button, Card } from "@drlife/ui";
import { localized } from "../localized";
import { formatPriceE4 } from "../money";
import { payableAmountE4, type BuyerOrder } from "../orders";
import type { PaymentAttempt } from "../payment";
import type { LocaleProps } from "./common";

export function PaymentView({
  t, language, order, attempt, actions, onBack, onPay, onRetry, onRefresh
}: {
  order: BuyerOrder;
  attempt: PaymentAttempt;
  actions?: ReactNode;
  onBack: () => void;
  onPay: () => void;
  onRetry: () => void;
  onRefresh: () => void;
} & LocaleProps) {
  useEffect(() => {
    if (attempt.status !== "confirming") return;
    const timer = window.setTimeout(onRefresh, 1000);
    return () => window.clearTimeout(timer);
  }, [attempt.status, onRefresh]);

  const payable = order.subOrders.reduce((total, item) => total + payableAmountE4(item), 0n);
  const [now] = useState(() => Date.now());
  const expired = attempt.status === "failed" && attempt.expiresAt !== undefined && Date.parse(attempt.expiresAt) <= now;
  const quoteDate = (value: string) => new Intl.DateTimeFormat(language, {
    dateStyle: "short",
    timeStyle: "short"
  }).format(new Date(value));
  return (
    <div className="buyer-page">
      <AppHeader slogan={t("payment.pageTitle")} actions={actions} />
      <main className="buyer-page__body">
        <Button variant="text" className="buyer-back" onClick={onBack}>{t("common.back")}</Button>
        <Card>
          <h1 className="buyer-section__title">{order.publicId}</h1>
          <p>{t("payment.number")}: {attempt.paymentNumber ?? t("payment.notStarted")}</p>
          {attempt.status === "confirming" && <p className="buyer-order-warning" role="status">{t("order.paymentConfirming")}</p>}
          {attempt.status === "succeeded" && <p role="status">{t("payment.succeeded")}</p>}
          {attempt.status === "failed" && <p className="buyer-cart-error" role="alert">{t(expired ? "payment.expired" : "payment.failed")}</p>}
          {attempt.status === "pending" && <Button className="buyer-cart-primary" onClick={onPay}>{t("payment.payNow")}</Button>}
          {attempt.status === "failed" && <Button className="buyer-cart-primary" onClick={onRetry}>{t("payment.retry")}</Button>}
        </Card>
        {order.subOrders.map((subOrder) => (
          <Card key={subOrder.publicId}>
            <h2 className="buyer-section__title">{localized(subOrder.merchantName, language)}</h2>
            {subOrder.lines.map((line) => (
              <div className="buyer-order-line" key={line.publicId}>
                <span lang={language}>{localized(line.name, language)} × {line.quantity}</span>
                <strong>{formatPriceE4(line.totalE4)}</strong>
              </div>
            ))}
            <div className="buyer-order-line">
              <span>{t("payment.shipping")}</span>
              <strong>{subOrder.shippingAmountE4 === 0n ? t("payment.freeShipping") : formatPriceE4(subOrder.shippingAmountE4)}</strong>
            </div>
            <small>{t("payment.quoteExpires", { date: quoteDate(subOrder.shippingQuoteExpiresAt) })}</small>
            <p className="buyer-order-total"><span>{t("payment.subtotal")}</span><strong>{formatPriceE4(payableAmountE4(subOrder))}</strong></p>
          </Card>
        ))}
        <Card>
          <p className="buyer-order-total"><span>{t("payment.payable")}</span><strong>{formatPriceE4(payable)}</strong></p>
        </Card>
      </main>
    </div>
  );
}
