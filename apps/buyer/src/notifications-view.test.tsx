// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider, t as translate } from "@drlife/i18n";
import type { BuyerApiClient } from "./api";
import { createNotificationIdempotencyKey } from "./notifications-view";
import { NotificationView } from "./v7-views";

const notification = { public_id: "01HZY0000000000000000NOTIF", event_id: "evt-1", event_type: "order.created", title: { zh: "订单", en: "Order", km: "បញ្ជាទិញ" }, body: { zh: "已创建", en: "Created", km: "បានបង្កើត" }, read_at: null, created_at: "2026-11-18T00:00:00Z" };

function fakeClient() {
  let allRead = false;
  const client = {
    GET: vi.fn((path: string) => path.includes("notification-settings")
      ? Promise.resolve({ data: { data: { items: [] } } })
      : Promise.resolve({ data: { data: { items: [{ ...notification, read_at: allRead ? "2026-11-18T00:01:00Z" : null }], unread_count: allRead ? 0 : 1 } } })),
    POST: vi.fn((path: string) => {
      if (path.endsWith("/read-all")) allRead = true;
      return Promise.resolve({ data: { data: { updated: true } } });
    }),
    PUT: vi.fn(() => Promise.resolve({ data: { data: { items: [{ event_type: "payment.confirmed", in_app_enabled: true, telegram_enabled: false }] } } }))
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

  it("renders default settings and sends read-all and setting updates", async () => {
    const client = fakeClient();
    render(<I18nProvider initialLanguage="en"><NotificationView client={client} language="en" t={(key, params) => translate(key, params, "en")} onBack={vi.fn()} onNavigate={vi.fn()} /></I18nProvider>);
    await screen.findByRole("switch", { name: /Payment confirmed/i });
    const orderSetting = screen.getAllByRole("switch", { name: /Payment confirmed/i }).at(-1)!;
    expect(screen.getAllByRole("switch").length).toBeGreaterThan(1);
    fireEvent.click(screen.getAllByRole("button", { name: "Mark all read" }).at(-1)!);
    await waitFor(() => expect(client.POST).toHaveBeenCalledWith("/v1/buyer/me/notifications/read-all", expect.anything()));
    const readAllRequest = (client.POST as unknown as ReturnType<typeof vi.fn>).mock.calls.find(([path]) => path === "/v1/buyer/me/notifications/read-all")?.[1] as { params: { header: { "Idempotency-Key": string } } };
    expect(readAllRequest.params.header["Idempotency-Key"]).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    await waitFor(() => expect(screen.queryByText("1")).toBeNull());
    fireEvent.click(orderSetting);
    await waitFor(() => expect(client.PUT).toHaveBeenCalledWith("/v1/buyer/me/notification-settings", expect.anything()));
    const settingRequest = (client.PUT as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[1] as { params: { header: { "Idempotency-Key": string } } };
    expect(settingRequest.params.header["Idempotency-Key"]).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    await waitFor(() => expect(orderSetting.getAttribute("aria-checked")).toBe("false"));
    expect(screen.getAllByRole("switch").slice(-5)).toHaveLength(5);
    expect(screen.queryByText("Withdrawal settled")).toBeNull();
    expect(screen.queryByText("Settlement settled")).toBeNull();
  });
});
