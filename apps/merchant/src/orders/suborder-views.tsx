import { Button, Card, EmptyState, ErrorState, ListItem, Loading, StatusTag } from "@drlife/ui";
import type { Language, TranslationKey } from "@drlife/i18n";
import { suborderStatuses, type MerchantSuborder, type SuborderStatus } from "./suborders";

export type Translate = (key: TranslationKey) => string;

function formatPriceE4(value: number): string {
  if (!Number.isSafeInteger(value) || value < 0 || value % 100 !== 0) return "—";
  const cents = value / 100;
  return `$${Math.floor(cents / 100)}.${String(cents % 100).padStart(2, "0")}`;
}

function statusKey(status: SuborderStatus): TranslationKey {
  return `order.merchantStatus.${status}` as TranslationKey;
}

function summary(order: MerchantSuborder): string {
  return order.items.map((item) => `${item.sku_public_id} × ${item.quantity}`).join(", ");
}

export function SuborderListView({
  translate,
  orders,
  selected,
  onSelectStatus,
  onOpen,
  loading = false,
  error = false,
  onRetry,
  hasMore = false,
  onLoadMore
}: {
  language: Language;
  translate: Translate;
  orders: readonly MerchantSuborder[];
  selected?: SuborderStatus;
  onSelectStatus: (status?: SuborderStatus) => void;
  onOpen: (order: MerchantSuborder) => void;
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
  hasMore?: boolean;
  onLoadMore?: () => void;
}) {
  const filtered = selected ? orders.filter((item) => item.status === selected) : orders;
  return <>
    <div className="merchant-order-filters">
      <Button variant="secondary" ariaPressed={selected === undefined} onClick={() => onSelectStatus()}>{translate("order.merchant.all")}</Button>
      {suborderStatuses.map((status) => <Button key={status} variant="secondary" ariaPressed={selected === status} onClick={() => onSelectStatus(status)}>{translate(statusKey(status))}</Button>)}
    </div>
    {loading && <Loading label={translate("common.loading")} skeleton />}
    {error && <ErrorState title={translate("common.failure")} description={translate("common.failure")} retryLabel={translate("common.retry")} onRetry={onRetry ?? (() => undefined)} />}
    {!loading && !error && filtered.length === 0 ? <EmptyState title={translate("order.merchant.listEmpty")} /> : null}
    {!loading && !error && filtered.length > 0 ? <Card><ul className="merchant-orders">
      {filtered.map((order) => <li key={order.public_id}><button type="button" className="merchant-order-item" onClick={() => onOpen(order)}>
        <ListItem title={order.public_id} description={`${summary(order)} · ${order.shipping_address.phone}`} trailing={<StatusTag tone="neutral">{translate(statusKey(order.status))}</StatusTag>} />
        <span className="merchant-order-item__total">{formatPriceE4(order.payable_amount_e4)}</span>
      </button></li>)}
    </ul>{hasMore && <Button variant="secondary" loading={loading} onClick={onLoadMore}>{translate("order.loadMore")}</Button>}</Card> : null}
  </>;
}

export function SuborderDetailView({ order, translate, onBack }: { order: MerchantSuborder; language: Language; translate: Translate; onBack: () => void }) {
  return <>
    <Button variant="secondary" className="merchant-back" onClick={onBack}>{translate("common.back")}</Button>
    <Card><div className="merchant-order-detail__heading"><h2>{order.public_id}</h2><StatusTag tone="neutral">{translate(statusKey(order.status))}</StatusTag></div>
      <p>{translate("order.buyer")}: {order.shipping_address.recipient_name}</p>
      <p>{translate("order.merchant.delivery")}: {translate(statusKey(order.status))}</p>
      <p>{order.shipping_address.phone}</p><p>{order.shipping_address.address_text}</p>
    </Card>
    <Card><h2>{translate("order.merchant.itemsTitle")}</h2>
      {order.items.map((item) => <div className="merchant-order-line" key={item.sku_public_id}><span>{item.sku_public_id} · {item.quantity} × {formatPriceE4(item.unit_price_e4)}</span><strong>{formatPriceE4(item.line_total_e4)}</strong></div>)}
      <p className="merchant-order-total"><span>{translate("order.merchant.total")}</span><strong>{formatPriceE4(order.payable_amount_e4)}</strong></p>
    </Card>
  </>;
}
