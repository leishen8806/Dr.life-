// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { I18nProvider } from "@drlife/i18n";
import { InventoryPage } from "./main";

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }),
});

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.defineProperty(window, "ResizeObserver", { writable: true, value: ResizeObserverMock });
Object.defineProperty(globalThis, "ResizeObserver", { writable: true, value: ResizeObserverMock });

const rows = [{ id: "i-1", product: "product.ceramicTile", zone: "A", location: "A-01-02", quantity: 120, available: 90, locationTotal: 100, phone: "+855 12 000 001", locations: "A-01-02: 100", movement: "IN-24001" }];
const renderInventory = (permissions: ("wms.inventory.read" | "wms.inventory.adjust")[] = ["wms.inventory.read"]) => render(<I18nProvider initialLanguage="zh"><InventoryPage permissions={permissions} initialRows={rows} /></I18nProvider>);

describe("InventoryPage rendering", () => {
  afterEach(() => document.body.replaceChildren());

  it("disables adjustment without wms.inventory.adjust", () => {
    renderInventory();
    expect(screen.getByRole("button", { name: "盘点调整" })).toHaveProperty("disabled", true);
  });

  it("rejects an empty adjustment reason", async () => {
    renderInventory(["wms.inventory.read", "wms.inventory.adjust"]);
    fireEvent.click(screen.getByText("120"));
    fireEvent.click(screen.getByRole("button", { name: "盘点调整" }));
    await waitFor(() => expect(document.querySelector('[role="dialog"] button[type="submit"]')).toBeTruthy());
    fireEvent.click(document.querySelector('[role="dialog"] button[type="submit"]') as HTMLButtonElement);
    await waitFor(() => expect(document.querySelector(".ant-form-item-has-error")).toBeTruthy());
  });

  it("marks a quantity mismatch row with row-mismatch", () => {
    renderInventory();
    expect(screen.getByText("120").closest("tr")?.className).toContain("row-mismatch");
  });
});
