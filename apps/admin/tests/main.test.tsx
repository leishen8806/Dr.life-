import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@drlife/i18n";
import { OrderPage, RejectReasonForm, ReviewTable } from "../src/main";
import type { OrderRecord, ReviewRecord } from "../src/api/types";

const mockApi = vi.hoisted(() => ({
  listReviews: vi.fn(),
  approveReview: vi.fn(),
  rejectReview: vi.fn(),
  listOrders: vi.fn()
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
  phone: "*** 1234",
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
    expect(screen.getByText("*** 1234")).toBeTruthy();
  });
});
