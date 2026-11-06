import { Card, EmptyState, ListItem, StatusTag } from "@drlife/ui";
import type { Language, TranslationKey } from "@drlife/i18n";
import { mockSuborders, suborderStatuses, type MerchantSuborder, type SuborderStatus } from "./suborders";

export type Translate = (key: TranslationKey) => string;

function formatPriceE4(value: bigint): string {
  const fraction = (value % 10_000n).toString().padStart(4, "0").replace(/0+$/, "");
  return `$${value / 10_000n}.${fraction.length < 2 ? fraction.padEnd(2, "0") : fraction}`;
}

function statusKey(status: SuborderStatus): TranslationKey {
  return `order.merchantStatus.${status}` as TranslationKey;
}

export function SuborderListView({
  language,
  translate,
  selected,
  onSelectStatus,
  onOpen
}: {
  language: Language;
  translate: Translate;
  selected?: SuborderStatus;
  onSelectStatus: (status?: SuborderStatus) => void;
  onOpen: (order: MerchantSuborder) => void;
}) {
  const orders = selected ? mockSuborders.filter((item) => item.status === selected) : mockSuborders;
  return (
    <>
      <div className="merchant-order-filters">
        <button type="button" aria-pressed={selected === undefined} onClick={() => onSelectStatus()}>{translate("order.merchant.all")}</button>
        {suborderStatuses.map((status) => (
          <button key={status} type="button" aria-pressed={selected === status} onClick={() => onSelectStatus(status)}>{translate(statusKey(status))}</button>
        ))}
      </div>
      {orders.length === 0 ? <EmptyState title={translate("order.merchant.listEmpty")} /> : (
        <Card>
          <ul className="merchant-orders">
            {orders.map((order) => (
              <li key={order.publicId}>
                <button type="button" className="merchant-order-item" onClick={() => onOpen(order)}>
                  <ListItem
                    title={order.publicId}
                    description={`${order.buyerName[language]} · ${order.productName[language]}`}
                    trailing={<StatusTag tone="neutral">{translate(statusKey(order.status))}</StatusTag>}
                  />
                  <span className="merchant-order-item__total">{formatPriceE4(order.totalE4)}</span>
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </>
  );
}

export function SuborderDetailView({ order, language, translate, onBack }: { order: MerchantSuborder; language: Language; translate: Translate; onBack: () => void }) {
  return (
    <>
      <button type="button" className="merchant-back" onClick={onBack}>{translate("common.back")}</button>
      <Card>
        <div className="merchant-order-detail__heading"><h2>{order.publicId}</h2><StatusTag tone="neutral">{translate(statusKey(order.status))}</StatusTag></div>
        <p>{translate("order.buyer")}: {order.buyerName[language]}</p>
        <p>{translate("order.merchant.delivery")}: {translate(`order.merchantStatus.${order.status}` as TranslationKey)}</p>
      </Card>
      <Card>
        <h2>{translate("order.merchant.itemsTitle")}</h2>
        <div className="merchant-order-line">
          <span lang={language}>{order.productName[language]} · {order.quantity} {order.unit} × {formatPriceE4(order.unitPriceE4)}</span>
          <strong>{formatPriceE4(order.totalE4)}</strong>
        </div>
        <p className="merchant-order-total"><span>{translate("order.merchant.total")}</span><strong>{formatPriceE4(order.totalE4)}</strong></p>
      </Card>
    </>
  );
}
