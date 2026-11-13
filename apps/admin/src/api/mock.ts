import type { AdminApi, OrderRecord, ReviewList } from "./types";

const reviews: ReviewList = {
  merchants: [
    { id: "m-001", nameKey: "admin.merchants", status: "pending", createdAt: "2026-11-13" },
    { id: "m-002", nameKey: "admin.merchants", status: "pending", createdAt: "2026-11-12" }
  ],
  products: [
    { id: "p-001", nameKey: "admin.products", status: "pending", createdAt: "2026-11-13" }
  ]
};
const orders: OrderRecord[] = [
  { id: "DL-20261113-001", buyerKey: "admin.buyer", merchantKey: "admin.merchants", amountE4: "125000", status: "attention", createdAt: "2026-11-13 09:20", deliveryKey: "admin.delivery", phone: "*** 1234", suborders: [{ id: "SO-001", productKey: "admin.products", status: "attention" }] },
  { id: "DL-20261112-006", buyerKey: "admin.buyer", merchantKey: "admin.merchants", amountE4: "86000", status: "normal", createdAt: "2026-11-12 16:40", deliveryKey: "admin.delivery", phone: "*** 8899", suborders: [{ id: "SO-006", productKey: "admin.products", status: "normal" }] }
];
const wait = () => new Promise((resolve) => setTimeout(resolve, 120));
export const mockApi: AdminApi = {
  async listReviews() { await wait(); return structuredClone(reviews); },
  async approveReview(kind, id) { await wait(); const item = reviews[kind === "merchant" ? "merchants" : "products"].find((entry) => entry.id === id); if (item) item.status = "approved"; },
  async rejectReview(kind, id) { await wait(); const item = reviews[kind === "merchant" ? "merchants" : "products"].find((entry) => entry.id === id); if (item) item.status = "rejected"; },
  async listOrders(filters = {}) { await wait(); return orders.filter((item) => (!filters.status || item.status === filters.status) && (!filters.query || `${item.id} ${item.buyerKey} ${item.merchantKey}`.toLowerCase().includes(filters.query.toLowerCase()))); },
  async getOrder(id) { await wait(); return orders.find((item) => item.id === id); }
};
