// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@drlife/i18n";
import type { createApiClient } from "@drlife/api-client";
import { MerchantNotificationsView } from "./notifications-view";

const notification = { public_id: "01HZY0000000000000000NOTIF", event_id: "evt-1", event_type: "order.created", title: { zh: "订单", en: "Order", km: "បញ្ជាទិញ" }, body: { zh: "已创建", en: "Created", km: "បានបង្កើត" }, read_at: null, created_at: "2026-11-18T00:00:00Z" };

function fakeClient() {
  const client = {
    GET: vi.fn((path: string) => path.includes("notification-settings")
      ? Promise.resolve({ data: { data: { items: [{ event_type: "order.created", in_app_enabled: true, telegram_enabled: true }] } } })
      : Promise.resolve({ data: { data: { items: [notification], unread_count: 1 } } })),
    POST: vi.fn(() => Promise.resolve({ data: { data: { updated: true } } })),
    PUT: vi.fn(() => Promise.resolve({ data: { data: { items: [{ event_type: "order.created", in_app_enabled: true, telegram_enabled: false }] } } }))
  };
  return client as unknown as ReturnType<typeof createApiClient>;
}

describe("MerchantNotificationsView", () => {
  it("loads real notification/settings endpoints and marks one item read", async () => {
    const client = fakeClient();
    render(<I18nProvider initialLanguage="en"><MerchantNotificationsView client={client} onBack={vi.fn()} /></I18nProvider>);
    expect(await screen.findByText("Order")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
    await waitFor(() => expect(client.POST).toHaveBeenCalledWith("/v1/merchant/me/notifications/{public_id}/read", expect.anything()));
  });
});
