// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { I18nProvider, type Language } from "@drlife/i18n";
import { DetailDrawer } from "./detail-drawer";

const record = { id: "r-1", reference: "ASN-24001", scheduled: 100, measured: 102, volume: 12, status: "pending-confirmation" };

describe("DetailDrawer enum rendering", () => {
  afterEach(cleanup);

  it.each([
    ["zh", "待商家确认"],
    ["en", "Merchant confirmation needed"],
    ["km", "រង់ចាំអ្នកលក់បញ្ជាក់"]
  ] as const)("renders the receiving status in %s", (language: Language, label: string) => {
    render(<I18nProvider initialLanguage={language}><DetailDrawer record={record} onClose={() => undefined} /></I18nProvider>);
    expect(screen.getByText(label)).toBeTruthy();
    expect(document.body.textContent).not.toContain("pending-confirmation");
  });

  it.each([
    ["zh", "中通", "自建配送", "已打包", "已派单"],
    ["en", "ZTO", "In-house", "Packed", "Dispatched"],
    ["km", "ZTO", "ដឹកជញ្ជូនផ្ទាល់ខ្លួន", "បានវេចខ្ចប់", "បានចាត់ចែង"]
  ] as const)("renders dispatch enums in %s", (language: Language, zto: string, inhouse: string, packed: string, dispatched: string) => {
    render(<I18nProvider initialLanguage={language}><DetailDrawer record={{ reference: "PK-1", deliveryMethod: "zto", status: "packed", driver: "Sokha" }} onClose={() => undefined} /></I18nProvider>);
    expect(screen.getByText(zto)).toBeTruthy();
    expect(screen.getByText(packed)).toBeTruthy();
    expect(document.body.textContent).not.toContain("zto");
    expect(document.body.textContent).not.toContain("packed");
    cleanup();
    render(<I18nProvider initialLanguage={language}><DetailDrawer record={{ reference: "PK-2", deliveryMethod: "inhouse", status: "dispatched", driver: "Sokha" }} onClose={() => undefined} /></I18nProvider>);
    expect(screen.getByText(inhouse)).toBeTruthy();
    expect(screen.getByText(dispatched)).toBeTruthy();
    expect(document.body.textContent).not.toContain("inhouse");
    expect(document.body.textContent).not.toContain("dispatched");
  });
});
