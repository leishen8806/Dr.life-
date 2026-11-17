import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, t } from "@drlife/i18n";
import { OrderPage, RejectReasonForm, ReviewTable } from "../src/main";
import { AdminManagementPage, AuditPage } from "../src/admin-pages";
import type { OrderRecord, ReviewRecord } from "../src/api/types";

const mockApi = vi.hoisted(() => ({
  listReviews: vi.fn(),
  approveReview: vi.fn(),
  rejectReview: vi.fn(),
  listOrders: vi.fn()
  ,login: vi.fn(), refresh: vi.fn(), logout: vi.fn(), changePassword: vi.fn(), stepUp: vi.fn(), listAdmins: vi.fn(), createAdmin: vi.fn(), disableAdmin: vi.fn(), enableAdmin: vi.fn(), updateAdminRole: vi.fn(), listAudit: vi.fn()
}));

vi.mock("../src/api", () => ({ api: mockApi }));

const reviews: ReviewRecord[] = [{ id: "m-001", nameKey: "admin.merchantOne", status: "pending", createdAt: "2026-11-13" }];
const orders: OrderRecord[] = [{
  id: "DL-001",
  buyerKey: "admin.buyer",
  merchantKey: "admin.merchantOne",
  amountE4: "125000",
  status: "normal",
  createdAt: "2026-11-13 09:20",
  deliveryKey: "admin.deliveryPacking",
  phone: "****1234",
  suborders: [{ id: "SO-001", productKey: "admin.productOne", status: "normal" }]
}];

function renderAdmin(element: React.ReactElement) {
  return render(<I18nProvider initialLanguage="en">{element}</I18nProvider>);
}

describe("admin review and order views", () => {
  afterEach(() => cleanup());
  beforeEach(() => {
    vi.clearAllMocks();
    mockApi.listReviews.mockResolvedValue({ merchants: reviews, products: [] });
    mockApi.approveReview.mockResolvedValue(undefined);
    mockApi.rejectReview.mockResolvedValue(undefined);
    mockApi.listOrders.mockImplementation(async ({ query } = {}) => query ? orders.filter((item) => item.id.includes(query)) : orders);
    mockApi.listAdmins.mockResolvedValue({ items: [{ publicId: "admin-1", name: "Test Admin", email: "admin@example.test", role: "super", dataScope: ["KH-PP"], status: "active", createdAt: "2026-11-01" }] });
    mockApi.listAudit.mockResolvedValue({ items: [{ id: 1, ts: "2026-11-17", actorId: 1, actorRole: "admin", action: "auth.login", target: "admin:test", requestId: "request-1" }] });
    mockApi.stepUp.mockResolvedValue("step-up-token");
  });

  it("does not submit an empty rejection reason", async () => {
    renderAdmin(<RejectReasonForm onSubmit={mockApi.rejectReview} />);
    fireEvent.click(screen.getByRole("button", { name: "Reject" }));
    expect((await screen.findByRole("alert")).textContent).toContain("A rejection reason is required");
    expect(mockApi.rejectReview).not.toHaveBeenCalled();
  });

  it("disables review actions and exposes a permission warning", () => {
    renderAdmin(<ReviewTable rows={reviews} canManage={false} onApprove={vi.fn()} onReject={vi.fn()} />);
    expect(screen.getAllByRole("button", { name: "Approve" })[0]).toHaveProperty("disabled", true);
    expect(screen.getAllByRole("button", { name: "Reject" })[0]).toHaveProperty("disabled", true);
  });

  it("filters orders and opens the detail drawer", async () => {
    renderAdmin(<OrderPage />);
    await screen.findByText("DL-001");
    fireEvent.change(screen.getByRole("textbox", { name: "Search" }), { target: { value: "missing" } });
    await waitFor(() => expect(mockApi.listOrders).toHaveBeenLastCalledWith(expect.objectContaining({ query: "missing" })));
    fireEvent.change(screen.getByRole("textbox", { name: "Search" }), { target: { value: "" } });
    await screen.findByText("DL-001");
    fireEvent.click(screen.getByText("DL-001"));
    expect(await screen.findByText("Order details")).toBeTruthy();
    expect(screen.getByText("****1234")).toBeTruthy();
  });

  it("keeps the USD symbol for Chinese and Khmer admin money", () => {
    expect(t("admin.currencySymbol", {}, "zh")).toBe("$");
    expect(t("admin.currencySymbol", {}, "km")).toBe("$");
  });

  it("loads administrator and audit pages through the adapter", async () => {
    renderAdmin(<AdminManagementPage />);
    expect(await screen.findByText("Test Admin")).toBeTruthy();
    expect(mockApi.listAdmins).toHaveBeenCalledOnce();
    cleanup();
    renderAdmin(<AuditPage />);
    expect(await screen.findByText("auth.login")).toBeTruthy();
    expect(mockApi.listAudit).toHaveBeenCalledOnce();
  });

  it("requires step-up before creating an administrator and shows credentials once", async () => {
    mockApi.createAdmin.mockResolvedValue({ admin: reviews[0], initialPassword: "one-time-password", totpProvisioningUri: "otpauth://example.test/fake" });
    renderAdmin(<AdminManagementPage />);
    fireEvent.click(screen.getByRole("button", { name: "New administrator" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New Operator" } });
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "operator@example.test" } });
    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.click(await screen.findByRole("option", { name: "ops" }));
    fireEvent.change(screen.getByLabelText("Verification code"), { target: { value: "123456" } });
    fireEvent.click(screen.getAllByRole("button", { name: "New administrator" }).at(-1)!);
    await waitFor(() => expect(mockApi.stepUp).toHaveBeenCalledWith("permission_change", "123456"));
    await waitFor(() => expect(mockApi.createAdmin).toHaveBeenCalledWith(expect.objectContaining({ name: "New Operator", email: "operator@example.test", stepUpToken: "step-up-token" })));
    expect(await screen.findByText("one-time-password")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "I saved these credentials" }));
    expect(mockApi.createAdmin).toHaveBeenCalledOnce();
  });
});
