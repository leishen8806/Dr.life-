import { useCallback, useEffect, useState } from "react";
import { Button, Loading } from "@drlife/ui";
import { useI18n, type Language, type TranslationKey } from "@drlife/i18n";
import type { BuyerApiClient } from "./api";

type Notification = import("@drlife/api-client").components["schemas"]["Notification"];
type Setting = import("@drlife/api-client").components["schemas"]["NotificationSetting"];

const eventLabels: Record<string, TranslationKey> = {
  "order.created": "notification.event.orderCreated", "payment.confirmed": "notification.event.paymentConfirmed",
  "order.shipped": "notification.event.orderShipped", "order.delivered": "notification.event.orderDelivered",
  "product.moderated": "notification.event.productModerated", "warehouse.received": "notification.event.warehouseReceived",
  "account.status_changed": "notification.event.accountStatusChanged", "agreement.updated": "notification.event.agreementUpdated",
  "withdrawal.settled": "notification.event.withdrawalSettled", "settlement.settled": "notification.event.settlementSettled"
};
const localized = (values: Record<string, string>, language: Language) => values[language] || values.en || values.zh || values.km || "";

export function BuyerNotificationsView({ client, onBack }: { client: BuyerApiClient; onBack: () => void }) {
  const { language, t } = useI18n();
  const [items, setItems] = useState<Notification[]>([]);
  const [settings, setSettings] = useState<Setting[]>([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    setLoading(true); setError(undefined);
    try {
      const [notifications, preferences] = await Promise.all([
        client.GET("/v1/buyer/me/notifications", { params: { query: { limit: 50 } } }),
        client.GET("/v1/buyer/me/notification-settings")
      ]);
      if (!notifications.data?.data || !preferences.data?.data) throw new Error("notification response missing");
      setItems(notifications.data.data.items); setUnread(notifications.data.data.unread_count); setSettings(preferences.data.data.items);
    } catch { setError(t("notification.loadFailed")); } finally { setLoading(false); }
  }, [client, t]);
  useEffect(() => { void load(); }, [load]);

  const markRead = async (publicId: string) => {
    await client.POST("/v1/buyer/me/notifications/{public_id}/read", { params: { path: { public_id: publicId.toLowerCase() }, header: { "Idempotency-Key": `notification-${publicId}` } } });
    setItems((current) => current.map((item) => item.public_id === publicId ? { ...item, read_at: item.read_at ?? new Date().toISOString() } : item));
    setUnread((current) => Math.max(0, current - 1));
  };
  const updateSetting = async (setting: Setting) => {
    const { data } = await client.PUT("/v1/buyer/me/notification-settings", { params: { header: { "Idempotency-Key": `setting-${setting.event_type}` } }, body: setting });
    if (data?.data) setSettings(data.data.items);
  };

  return <section aria-label={t("notification.title")} className="notification-center">
    <Button variant="secondary" onClick={onBack}>{t("notification.back")}</Button>
    <h1>{t("notification.title")} {unread > 0 ? `(${unread})` : ""}</h1>
    {loading ? <Loading label={t("common.loading")} /> : error ? <p role="alert">{error} <Button variant="secondary" onClick={() => void load()}>{t("notification.retry")}</Button></p> : <>
      <Button variant="secondary" onClick={async () => { await client.POST("/v1/buyer/me/notifications/read-all", { params: { header: { "Idempotency-Key": "notification-read-all" } } }); await load(); }}>{t("notification.markAllRead")}</Button>
      <ul>{items.map((item) => <li key={item.public_id} data-unread={!item.read_at}>
        <strong>{localized(item.title, language)}</strong><p>{localized(item.body, language)}</p>
        {!item.read_at ? <Button variant="secondary" onClick={() => void markRead(item.public_id)}>{t("notification.markRead")}</Button> : null}
      </li>)}</ul>
      <h2>{t("notification.settings")}</h2>
      {settings.map((setting) => <label key={setting.event_type}>
        <input type="checkbox" checked={setting.telegram_enabled} onChange={(event) => void updateSetting({ ...setting, telegram_enabled: event.target.checked })} /> {t(eventLabels[setting.event_type] ?? "notification.title")} {t("notification.telegram")}
      </label>)}
    </>}
  </section>;
}
