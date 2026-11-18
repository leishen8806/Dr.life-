import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Card,
  EmptyState,
  FormField,
  ListItem,
  NotificationList,
  StatusTag,
  Switch
} from "@drlife/ui";
import type { Language, TranslationKey } from "@drlife/i18n";
import type { BuyerApiClient } from "./api";
import { createNotificationIdempotencyKey } from "./notifications-view";

type Translate = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string;

const categories = [
  "design",
  "kitchen",
  "bathroom",
  "flooring",
  "lighting",
  "furniture"
] as const;
const companies = [
  {
    id: "lotus",
    name: "Lotus Build",
    region: "Phnom Penh",
    specialties: ["design", "kitchen"],
    description: "Residential renovation and custom joinery.",
    products: ["Kitchen cabinet", "Oak flooring"]
  },
  {
    id: "mekong",
    name: "Mekong Home",
    region: "Kandal",
    specialties: ["bathroom", "lighting"],
    description: "Practical materials for family homes.",
    products: ["Wall tile", "LED panel"]
  }
];
const cases = [
  {
    id: "case-1",
    title: "Warm apartment",
    area: "82 m²",
    style: "Modern",
    company: "Lotus Build"
  },
  {
    id: "case-2",
    title: "Mekong riverside home",
    area: "146 m²",
    style: "Natural",
    company: "Mekong Home"
  }
];

export interface InquiryDraft {
  description: string;
  budget: string;
  duration: string;
  region: string;
  contact: "telegram" | "phone" | "email";
}
export function validateInquiry(draft: InquiryDraft, t: Translate): string[] {
  return [
    !draft.description.trim() ? t("validation.required") : "",
    !draft.budget.trim() ? t("validation.required") : "",
    !draft.duration.trim() ? t("validation.required") : "",
    !draft.region.trim() ? t("validation.required") : ""
  ].filter(Boolean);
}

export function DecorView({
  t,
  onBack,
  onOpenNotifications
}: {
  language?: Language;
  t: Translate;
  onBack: () => void;
  onOpenNotifications: () => void;
}) {
  const [tab, setTab] = useState<"companies" | "cases">("companies");
  const [query, setQuery] = useState("");
  const [region, setRegion] = useState("all");
  const [category, setCategory] = useState<string>();
  const [selectedCompany, setSelectedCompany] =
    useState<(typeof companies)[number]>();
  const [selectedCase, setSelectedCase] = useState<(typeof cases)[number]>();
  const [inquiry, setInquiry] = useState<InquiryDraft>({
    description: "",
    budget: "",
    duration: "",
    region: "",
    contact: "telegram"
  });
  const [submitted, setSubmitted] = useState(false);
  const [inquiryAttempted, setInquiryAttempted] = useState(false);
  const [inquiryTouched, setInquiryTouched] = useState<Record<string, boolean>>(
    {}
  );
  const filteredCompanies = useMemo(
    () =>
      companies.filter(
        (item) =>
          (!query ||
            `${item.name} ${item.description}`
              .toLowerCase()
              .includes(query.toLowerCase())) &&
          (region === "all" || item.region === region) &&
          (!category || item.specialties.includes(category as never))
      ),
    [category, query, region]
  );
  const errors = validateInquiry(inquiry, t);
  const fieldErrors = [
    !inquiry.description.trim() ? t("validation.required") : "",
    !inquiry.budget.trim() ? t("validation.required") : "",
    !inquiry.duration.trim() ? t("validation.required") : "",
    !inquiry.region.trim() ? t("validation.required") : ""
  ];
  const inquiryFields = ["description", "budget", "duration", "region"];
  const visibleErrors = fieldErrors.map((error, index) =>
    inquiryAttempted || inquiryTouched[inquiryFields[index]!] ? error : ""
  );
  if (selectedCase)
    return (
      <main className="buyer-page">
        <section className="buyer-page__body">
          <Button
            testId="buyer-back"
            variant="text"
            onClick={() => setSelectedCase(undefined)}
          >
            {t("common.back")}
          </Button>
          <Card>
            <h1 className="buyer-section__title">{selectedCase.title}</h1>
            <p>
              {selectedCase.area} · {selectedCase.style}
            </p>
            <p>{selectedCase.company}</p>
            <div className="v7-case-image" aria-label={t("buyer.caseImage")}>
              {t("buyer.caseImage")}
            </div>
          </Card>
        </section>
      </main>
    );
  if (selectedCompany)
    return (
      <main className="buyer-page">
        <section className="buyer-page__body">
          <Button
            testId="buyer-back"
            variant="text"
            onClick={() => setSelectedCompany(undefined)}
          >
            {t("common.back")}
          </Button>
          <Card>
            <h1 className="buyer-section__title">{selectedCompany.name}</h1>
            <p>{selectedCompany.description}</p>
            <StatusTag tone="success">{selectedCompany.region}</StatusTag>
            <h2>{t("buyer.products")}</h2>
            <div className="v7-horizontal-list">
              {selectedCompany.products.map((product) => (
                <Card key={product}>
                  <strong>{product}</strong>
                  <small>{t("buyer.available")}</small>
                </Card>
              ))}
            </div>
            <h2>{t("buyer.inquiry")}</h2>
            {submitted ? (
              <p role="status">{t("buyer.inquirySent")}</p>
            ) : (
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  setInquiryAttempted(true);
                  if (!import.meta.env.DEV) return;
                  if (!errors.length) setSubmitted(true);
                }}
              >
                <FormField
                  label={t("buyer.description")}
                  error={visibleErrors[0]}
                >
                  <textarea
                    value={inquiry.description}
                    onBlur={() =>
                      setInquiryTouched({
                        ...inquiryTouched,
                        description: true
                      })
                    }
                    onChange={(event) =>
                      setInquiry({
                        ...inquiry,
                        description: event.target.value
                      })
                    }
                  />
                </FormField>
                <FormField label={t("buyer.budget")} error={visibleErrors[1]}>
                  <input
                    value={inquiry.budget}
                    onBlur={() =>
                      setInquiryTouched({ ...inquiryTouched, budget: true })
                    }
                    onChange={(event) =>
                      setInquiry({ ...inquiry, budget: event.target.value })
                    }
                  />
                </FormField>
                <FormField label={t("buyer.duration")} error={visibleErrors[2]}>
                  <input
                    value={inquiry.duration}
                    onBlur={() =>
                      setInquiryTouched({ ...inquiryTouched, duration: true })
                    }
                    onChange={(event) =>
                      setInquiry({ ...inquiry, duration: event.target.value })
                    }
                  />
                </FormField>
                <FormField label={t("buyer.region")} error={visibleErrors[3]}>
                  <input
                    value={inquiry.region}
                    onBlur={() =>
                      setInquiryTouched({ ...inquiryTouched, region: true })
                    }
                    onChange={(event) =>
                      setInquiry({ ...inquiry, region: event.target.value })
                    }
                  />
                </FormField>
                <fieldset>
                  <legend>{t("buyer.contact")}</legend>
                  {(["telegram", "phone", "email"] as const).map((value) => (
                    <label key={value}>
                      <input
                        type="radio"
                        checked={inquiry.contact === value}
                        onChange={() =>
                          setInquiry({ ...inquiry, contact: value })
                        }
                      />
                      {t(`buyer.contact.${value}` as TranslationKey)}
                    </label>
                  ))}
                </fieldset>
                {!import.meta.env.DEV && (
                  <p role="status">{t("common.comingSoon")}</p>
                )}
                <Button type="submit" disabled={!import.meta.env.DEV}>
                  {t("common.submit")}
                </Button>
              </form>
            )}
          </Card>
        </section>
      </main>
    );
  return (
    <main className="buyer-page">
      <section className="buyer-page__body">
        <div className="v7-page-heading">
          <Button testId="buyer-back" variant="text" onClick={onBack}>
            {t("common.back")}
          </Button>
          <Button variant="secondary" onClick={onOpenNotifications}>
            {t("buyer.notifications")}
          </Button>
        </div>
        <div className="v7-tabs" role="tablist">
          <button
            type="button"
            data-testid="buyer-tab-companies"
            aria-pressed={tab === "companies"}
            onClick={() => setTab("companies")}
          >
            {t("buyer.companies")}
          </button>
          <button
            type="button"
            data-testid="buyer-tab-cases"
            aria-pressed={tab === "cases"}
            onClick={() => setTab("cases")}
          >
            {t("buyer.cases")}
          </button>
        </div>
        {tab === "companies" ? (
          <>
            <div className="v7-search-row">
              <input
                aria-label={t("common.search")}
                placeholder={t("common.search")}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
              <select
                aria-label={t("buyer.region")}
                value={region}
                onChange={(event) => setRegion(event.target.value)}
              >
                <option value="all">{t("admin.all")}</option>
                <option value="Phnom Penh">Phnom Penh</option>
                <option value="Kandal">Kandal</option>
              </select>
            </div>
            <div className="v7-category-grid">
              {categories.map((item) => (
                <button
                  type="button"
                  key={item}
                  aria-pressed={category === item}
                  onClick={() =>
                    setCategory(category === item ? undefined : item)
                  }
                >
                  {t(`buyer.category.${item}` as TranslationKey)}
                </button>
              ))}
            </div>
            {filteredCompanies.length ? (
              filteredCompanies.map((item) => (
                <Card key={item.id}>
                  <ListItem
                    testId="buyer-company"
                    title={item.name}
                    description={`${item.region} · ${item.description}`}
                    trailing={t("buyer.viewCompany")}
                    onClick={() => setSelectedCompany(item)}
                  />
                </Card>
              ))
            ) : (
              <EmptyState title={t("common.empty")} />
            )}
          </>
        ) : (
          <div className="v7-case-grid">
            {cases.map((item) => (
              <Card key={item.id}>
                <button
                  type="button"
                  data-testid={`buyer-case-${item.id}`}
                  className="v7-card-button"
                  onClick={() => setSelectedCase(item)}
                >
                  <div className="v7-case-image">{t("buyer.caseImage")}</div>
                  <strong>{item.title}</strong>
                  <small>
                    {item.area} · {item.style}
                  </small>
                </button>
              </Card>
            ))}
          </div>
        )}
      </section>
    </main>
  );
}

export { AftercareView } from "./v7-aftercare-view";

export interface NotificationItem {
  id: string;
  type: "order" | "wallet" | "product" | "warehouse" | "profile";
  titleKey: TranslationKey;
  unread: boolean;
  target: string;
}
export const mockNotifications: NotificationItem[] = [
  {
    id: "n1",
    type: "order",
    titleKey: "buyer.notice.order",
    unread: true,
    target: "order"
  },
  {
    id: "n2",
    type: "product",
    titleKey: "buyer.notice.product",
    unread: true,
    target: "profile"
  }
];
type ApiNotification =
  import("@drlife/api-client").components["schemas"]["Notification"];
type ApiSetting =
  import("@drlife/api-client").components["schemas"]["NotificationSetting"];
const notificationSettingLabels: Record<string, TranslationKey> = {
  "payment.confirmed": "notification.event.paymentConfirmed",
  "order.shipped": "notification.event.orderShipped",
  "order.delivered": "notification.event.orderDelivered",
  "account.status_changed": "notification.event.accountStatusChanged",
  "agreement.updated": "notification.event.agreementUpdated"
};
const defaultBuyerNotificationSettings: ApiSetting[] = Object.keys(
  notificationSettingLabels
).map((event_type) => ({
  event_type,
  in_app_enabled: true,
  telegram_enabled: true
}));
const mergeNotificationSettings = (items: ApiSetting[]) =>
  defaultBuyerNotificationSettings.map(
    (fallback) =>
      items.find((item) => item.event_type === fallback.event_type) ?? fallback
  );
const localized = (values: Record<string, string>, language: Language) =>
  values[language] || values.en || values.zh || values.km || "";

export function NotificationView({
  client,
  language,
  t,
  onBack,
  onNavigate,
  onUnreadCountChange
}: {
  client: BuyerApiClient;
  language: Language;
  t: Translate;
  onBack: () => void;
  onNavigate: (target: string) => void;
  onUnreadCountChange?: (count: number) => void;
}) {
  const [items, setItems] = useState<ApiNotification[]>([]);
  const [settings, setSettings] = useState<ApiSetting[]>([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>();
  const load = useCallback(async () => {
    setLoading(true);
    setError(undefined);
    try {
      const [notifications, preferences] = await Promise.all([
        client.GET("/v1/buyer/me/notifications", {
          params: { query: { limit: 50 } }
        }),
        client.GET("/v1/buyer/me/notification-settings")
      ]);
      if (!notifications.data?.data || !preferences.data?.data)
        throw new Error("notification response missing");
      setItems(notifications.data.data.items);
      setUnread(notifications.data.data.unread_count);
      onUnreadCountChange?.(notifications.data.data.unread_count);
      setSettings(mergeNotificationSettings(preferences.data.data.items));
    } catch {
      setError(t("notification.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [client, onUnreadCountChange, t]);
  useEffect(() => {
    void load();
  }, [load]);
  const markRead = async (publicId: string) => {
    try {
      const result = await client.POST(
        "/v1/buyer/me/notifications/{public_id}/read",
        {
          params: {
            path: { public_id: publicId.toLowerCase() },
            header: {
              "Idempotency-Key":
                createNotificationIdempotencyKey("notification-read")
            }
          }
        }
      );
      if (result.error) throw new Error("notification read failed");
      const wasUnread =
        items.find((item) => item.public_id === publicId)?.read_at == null;
      setItems((current) =>
        current.map((item) =>
          item.public_id === publicId
            ? { ...item, read_at: item.read_at ?? new Date().toISOString() }
            : item
        )
      );
      if (wasUnread) {
        setUnread((current) => {
          const next = Math.max(0, current - 1);
          onUnreadCountChange?.(next);
          return next;
        });
      }
    } catch {
      setError(t("notification.actionFailed"));
    }
  };
  const updateSetting = async (setting: ApiSetting) => {
    try {
      const { data, error: requestError } = await client.PUT(
        "/v1/buyer/me/notification-settings",
        {
          params: {
            header: {
              "Idempotency-Key": createNotificationIdempotencyKey(
                "notification-setting"
              )
            }
          },
          body: setting
        }
      );
      if (requestError) throw new Error("notification setting failed");
      if (data?.data) setSettings(mergeNotificationSettings(data.data.items));
    } catch {
      setError(t("notification.actionFailed"));
    }
  };
  const select = (id: string) => {
    const item = items.find((entry) => entry.public_id === id);
    void markRead(id);
    if (item)
      onNavigate(item.event_type.startsWith("order.") ? "orders" : "profile");
  };
  const markAllRead = async () => {
    try {
      const result = await client.POST("/v1/buyer/me/notifications/read-all", {
        params: {
          header: {
            "Idempotency-Key": createNotificationIdempotencyKey(
              "notification-read-all"
            )
          }
        }
      });
      if (result.error) throw new Error("notification read-all failed");
      await load();
    } catch {
      setError(t("notification.actionFailed"));
    }
  };
  return (
    <main className="buyer-page">
      <section className="buyer-page__body">
        <Button testId="buyer-back" variant="text" onClick={onBack}>
          {t("common.back")}
        </Button>
        <Card>
          <div className="v7-page-heading">
            <h1 className="buyer-section__title">{t("buyer.notifications")}</h1>
            {unread ? <StatusTag tone="warning">{unread}</StatusTag> : null}
          </div>
          {loading ? (
            <p role="status">{t("common.loading")}</p>
          ) : error ? (
            <p role="alert">
              {error}{" "}
              <Button variant="secondary" onClick={() => void load()}>
                {t("notification.retry")}
              </Button>
            </p>
          ) : (
            <>
              <NotificationList
                items={items.map((item) => ({
                  id: item.public_id,
                  title: localized(item.title, language),
                  description: localized(item.body, language),
                  unread: !item.read_at
                }))}
                markAllLabel={t("buyer.markAllRead")}
                onMarkAllRead={() => void markAllRead()}
                onSelect={select}
              />
              <h2 className="buyer-section__title">
                {t("notification.settings")}
              </h2>
              {settings.map((setting) => (
                <label
                  className="buyer-notification-setting"
                  key={setting.event_type}
                >
                  <span>
                    {t(
                      notificationSettingLabels[setting.event_type] ??
                        "notification.title"
                    )}{" "}
                    {t("notification.telegram")}
                  </span>
                  <Switch
                    checked={setting.telegram_enabled}
                    label={t(
                      notificationSettingLabels[setting.event_type] ??
                        "notification.title"
                    )}
                    onChange={(checked) =>
                      void updateSetting({
                        ...setting,
                        telegram_enabled: checked
                      })
                    }
                  />
                </label>
              ))}
            </>
          )}
        </Card>
      </section>
    </main>
  );
}
