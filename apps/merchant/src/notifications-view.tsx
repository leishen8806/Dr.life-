import { useCallback, useEffect, useState } from "react";
import { Button, Loading } from "@drlife/ui";
import type { createApiClient } from "@drlife/api-client";

type Notification = import("@drlife/api-client").components["schemas"]["Notification"];
type Setting = import("@drlife/api-client").components["schemas"]["NotificationSetting"];
type Client = ReturnType<typeof createApiClient>;

export function MerchantNotificationsView({ client, onBack }: { client: Client; onBack: () => void }) {
  const [items, setItems] = useState<Notification[]>([]); const [settings, setSettings] = useState<Setting[]>([]); const [unread, setUnread] = useState(0); const [loading, setLoading] = useState(true); const [error, setError] = useState<string>();
  const load = useCallback(async () => {
    setLoading(true); setError(undefined);
    try {
      const [notifications, preferences] = await Promise.all([client.GET("/v1/merchant/me/notifications", { params: { query: { limit: 50 } } }), client.GET("/v1/merchant/me/notification-settings")]);
      if (!notifications.data?.data || !preferences.data?.data) throw new Error("notification response missing");
      setItems(notifications.data.data.items); setUnread(notifications.data.data.unread_count); setSettings(preferences.data.data.items);
    } catch { setError("Unable to load notifications"); } finally { setLoading(false); }
  }, [client]);
  useEffect(() => { void load(); }, [load]);
  const markRead = async (publicId: string) => { await client.POST("/v1/merchant/me/notifications/{public_id}/read", { params: { path: { public_id: publicId.toLowerCase() }, header: { "Idempotency-Key": `notification-${publicId}` } } }); setItems((current) => current.map((item) => item.public_id === publicId ? { ...item, read_at: item.read_at ?? new Date().toISOString() } : item)); setUnread((current) => Math.max(0, current - 1)); };
  const updateSetting = async (setting: Setting) => { const { data } = await client.PUT("/v1/merchant/me/notification-settings", { params: { header: { "Idempotency-Key": `setting-${setting.event_type}` } }, body: setting }); if (data?.data) setSettings(data.data.items); };
  return <section aria-label="Merchant notifications" className="notification-center">
    <Button variant="secondary" onClick={onBack}>Back</Button><h1>Notifications {unread > 0 ? `(${unread})` : ""}</h1>
    {loading ? <Loading label="Loading notifications" /> : error ? <p role="alert">{error} <Button variant="secondary" onClick={() => void load()}>Retry</Button></p> : <>
      <Button variant="secondary" onClick={async () => { await client.POST("/v1/merchant/me/notifications/read-all", { params: { header: { "Idempotency-Key": "notification-read-all" } } }); await load(); }}>Mark all read</Button>
      <ul>{items.map((item) => <li key={item.public_id} data-unread={!item.read_at}><strong>{item.title.en || item.title.zh}</strong><p>{item.body.en || item.body.zh}</p>{!item.read_at ? <Button variant="secondary" onClick={() => void markRead(item.public_id)}>Mark read</Button> : null}</li>)}</ul>
      <h2>Notification settings</h2>{settings.map((setting) => <label key={setting.event_type}><input type="checkbox" checked={setting.telegram_enabled} onChange={(event) => void updateSetting({ ...setting, telegram_enabled: event.target.checked })} /> {setting.event_type} Telegram</label>)}
    </>}
  </section>;
}
