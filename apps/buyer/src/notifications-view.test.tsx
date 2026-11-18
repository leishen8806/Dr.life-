// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider, t as translate } from "@drlife/i18n";
import type { BuyerApiClient } from "./api";
import { createNotificationIdempotencyKey } from "./notifications-view";
import { NotificationView } from "./v7-views";

const notification = { public_id: "01HZY0000000000000000NOTIF", event_id: "evt-1", event_type: "order.created", title: { zh: "订单", en: "Order", km: "បញ្ជាទិញ" }, body: { zh: "已创建", en: "Created", km: "បានបង្កើត" }, read_at: null, created_at: "2026-11-18T00:00:00Z" };

function fakeClient() {
  const client = {
    GET: vi.fn((path: string) => path.includes("notification-settings")
      ? Promise.resolve({ data: { data: { items: [{ event_type: "order.created", in_app_enabled: true, telegram_enabled: true }] } } })
      : Promise.resolve({ data: { data: { items: [notification], unread_count: 1 } } })),
    POST: vi.fn(() => Promise.resolve({ data: { data: { updated: true } } })),
    PUT: vi.fn(() => Promise.resolve({ data: { data: { items: [{ event_type: "order.created", in_app_enabled: true, telegram_enabled: false }] } } }))
  };
  return client as unknown as BuyerApiClient;
}

describe("Buyer V7 NotificationView", () => {
  it("loads real notification/settings endpoints and marks one item read", async () => {
    const client = fakeClient();
    render(<I18nProvider initialLanguage="en"><NotificationView client={client} language="en" t={(key, params) => translate(key, params, "en")} onBack={vi.fn()} onNavigate={vi.fn()} /></I18nProvider>);
    expect(await screen.findByText("Order")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: /Order/ }));
    await waitFor(() => expect(client.POST).toHaveBeenCalledWith("/v1/buyer/me/notifications/{public_id}/read", expect.anything()));
    const readRequest = (client.POST as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[1] as { params: { path: { public_id: string }; header: { "Idempotency-Key": string } } };
    expect(readRequest.params.path.public_id).toBe(notification.public_id.toLowerCase());
    expect(readRequest.params.header["Idempotency-Key"]).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
  });

  it("creates a fresh valid idempotency key for each setting change", () => {
    const keys = [createNotificationIdempotencyKey("notification-setting"), createNotificationIdempotencyKey("notification-setting")];
    expect(keys[0]).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    expect(keys[1]).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    expect(keys[0]).not.toBe(keys[1]);
  });
});
