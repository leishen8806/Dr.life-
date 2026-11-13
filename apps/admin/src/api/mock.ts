import type { AdminApi, OrderRecord, ReviewList } from "./types";

const reviews: ReviewList = {
  merchants: [
    { id: "m-001", nameKey: "admin.merchantOne", status: "pending", createdAt: "2026-11-13" },
    { id: "m-002", nameKey: "admin.merchantTwo", status: "pending", createdAt: "2026-11-12" }
  ],
  products: [
    { id: "p-001", nameKey: "admin.productOne", status: "pending", createdAt: "2026-11-13" }
  ]
};
const orders: OrderRecord[] = [
  { id: "DL-20261113-001", buyerKey: "admin.buyerOne", merchantKey: "admin.merchantOne", amountE4: "125000", status: "attention", createdAt: "2026-11-13 09:20", deliveryKey: "admin.deliveryPacking", phone: "*** 1234", suborders: [{ id: "SO-001", productKey: "admin.productOne", status: "attention" }] },
  { id: "DL-20261112-006", buyerKey: "admin.buyerTwo", merchantKey: "admin.merchantTwo", amountE4: "86000", status: "normal", createdAt: "2026-11-12 16:40", deliveryKey: "admin.deliveryShipped", phone: "*** 8899", suborders: [{ id: "SO-006", productKey: "admin.productTwo", status: "normal" }] }
];
const wait = () => new Promise((resolve) => setTimeout(resolve, 120));
export const mockApi: AdminApi = {
  async listReviews() { await wait(); return structuredClone(reviews); },
  async approveReview(kind, id) { await wait(); const item = reviews[kind === "merchant" ? "merchants" : "products"].find((entry) => entry.id === id); if (item) item.status = "approved"; },
  async rejectReview(kind, id) { await wait(); const item = reviews[kind === "merchant" ? "merchants" : "products"].find((entry) => entry.id === id); if (item) item.status = "rejected"; },
  async listOrders(filters = {}) { await wait(); return orders.filter((item) => (!filters.status || item.status === filters.status) && (!filters.query || (item.id + " " + item.buyerKey + " " + item.merchantKey).toLowerCase().includes(filters.query.toLowerCase())) && (!filters.from || item.createdAt.slice(0, 10) >= filters.from) && (!filters.to || item.createdAt.slice(0, 10) <= filters.to)); },
  async getOrder(id) { await wait(); return orders.find((item) => item.id === id); }
};
