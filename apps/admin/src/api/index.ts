import type { AdminApi } from "./types";

async function getApi(): Promise<AdminApi> {
  if (import.meta.env.DEV) return (await import("./mock")).mockApi;
  return {
    async listReviews() { return { merchants: [], products: [] }; },
    async approveReview() { /* TODO: connect to the approved OpenAPI fields after backend confirmation. */ },
    async rejectReview() { /* TODO: connect to the approved OpenAPI fields after backend confirmation. */ },
    async listOrders() { return []; },
    async getOrder() { return undefined; },
    async listAgreements() { return []; },
    async listAgreementVersions() { return []; },
    async saveAgreementDraft() { throw new Error("backend_field_mapping_pending"); },
    async publishAgreement() { throw new Error("backend_field_mapping_pending"); },
    async listIntegrations() { return []; },
    async updateIntegration() { throw new Error("backend_field_mapping_pending"); },
    async testIntegration() { throw new Error("backend_field_mapping_pending"); },
    async enableIntegration() { throw new Error("backend_field_mapping_pending"); },
    async listIntegrationCalls() { return []; }
  };
}
export const api = {
  listReviews: () => getApi().then((client) => client.listReviews()),
  approveReview: (kind: "merchant" | "product", id: string) => getApi().then((client) => client.approveReview(kind, id)),
  rejectReview: (kind: "merchant" | "product", id: string, reason: string) => getApi().then((client) => client.rejectReview(kind, id, reason)),
  listOrders: (filters?: Parameters<AdminApi["listOrders"]>[0]) => getApi().then((client) => client.listOrders(filters)),
  getOrder: (id: string) => getApi().then((client) => client.getOrder(id))
  ,listAgreements: () => getApi().then((client) => client.listAgreements())
  ,listAgreementVersions: (id: string) => getApi().then((client) => client.listAgreementVersions(id))
  ,saveAgreementDraft: (id: string, language: "zh" | "en" | "km", value: string) => getApi().then((client) => client.saveAgreementDraft(id, language, value))
  ,publishAgreement: (id: string, input: { changeNote: string; effectiveDate: string }) => getApi().then((client) => client.publishAgreement(id, input))
  ,listIntegrations: () => getApi().then((client) => client.listIntegrations())
  ,updateIntegration: (id: string, input: { endpoint: string; environment: "test" | "production"; secret?: string }) => getApi().then((client) => client.updateIntegration(id, input))
  ,testIntegration: (id: string) => getApi().then((client) => client.testIntegration(id))
  ,enableIntegration: (id: string) => getApi().then((client) => client.enableIntegration(id))
  ,listIntegrationCalls: (id: string) => getApi().then((client) => client.listIntegrationCalls(id))
};
