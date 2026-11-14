import type { AdminApi } from "./types";

async function getApi(): Promise<AdminApi> {
  if (import.meta.env.DEV) return (await import("./mock")).mockApi;
  return {
    async listReviews() { return { merchants: [], products: [] }; },
    async approveReview() { /* TODO: connect to the approved OpenAPI fields after backend confirmation. */ },
    async rejectReview() { /* TODO: connect to the approved OpenAPI fields after backend confirmation. */ },
    async listOrders() { return []; },
    async getOrder() { return undefined; }
  };
}
export const api = {
  listReviews: () => getApi().then((client) => client.listReviews()),
  approveReview: (kind: "merchant" | "product", id: string) => getApi().then((client) => client.approveReview(kind, id)),
  rejectReview: (kind: "merchant" | "product", id: string, reason: string) => getApi().then((client) => client.rejectReview(kind, id, reason)),
  listOrders: (filters?: Parameters<AdminApi["listOrders"]>[0]) => getApi().then((client) => client.listOrders(filters)),
  getOrder: (id: string) => getApi().then((client) => client.getOrder(id))
};
