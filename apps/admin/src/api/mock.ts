import type { AdminApi, AdminUserRecord, AgreementRecord, AgreementVersionRecord, AuditRecord, IntegrationCallRecord, IntegrationRecord, OrderRecord, ReviewList } from "./types";

const mockAdmins: AdminUserRecord[] = [{ publicId: "01ARZ3NDEKTSV4RRFFQ69G5FAA", name: "Demo administrator", email: "admin@example.test", role: "super", dataScope: ["KH-PP"], status: "active", createdAt: "2026-11-01T00:00:00Z" }];
const mockAudit: AuditRecord[] = [{ id: 1, ts: "2026-11-17T00:00:00Z", actorId: 1, actorRole: "admin", action: "auth.login", target: "admin:demo", detail: {}, requestId: "mock-request" }];
let mockLoggedIn = false;

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
  { id: "DL-20261113-001", buyerKey: "admin.buyerOne", merchantKey: "admin.merchantOne", amountE4: "125000", status: "attention", createdAt: "2026-11-13 09:20", deliveryKey: "admin.deliveryPending", phone: "****1234", suborders: [{ id: "SO-001", productKey: "admin.productOne", status: "attention" }] },
  { id: "DL-20261112-006", buyerKey: "admin.buyerTwo", merchantKey: "admin.merchantTwo", amountE4: "86000", status: "normal", createdAt: "2026-11-12 16:40", deliveryKey: "admin.deliveryShipped", phone: "****8899", suborders: [{ id: "SO-006", productKey: "admin.productTwo", status: "normal" }] }
];
const agreements: AgreementRecord[] = [
  { id: "privacy-v3", kind: "privacy", version: "v3", effectiveDate: "2026-11-01", state: "published", consented: 128, required: 140, languages: { zh: "示例条款（模拟数据，非正式法律文本）", en: "Example terms (mock data, not formal legal text)", km: "គំរូលក្ខខណ្ឌ (ទិន្នន័យសាកល្បង មិនមែនអត្ថបទច្បាប់ផ្លូវការ)" } },
  { id: "terms-v2", kind: "terms", version: "v2", effectiveDate: "2026-10-15", state: "draft", consented: 0, required: 140, languages: { zh: "", en: "", km: "" } }
];
const agreementVersions: AgreementVersionRecord[] = [
  { id: "privacy-v3-history", agreementId: "privacy-v3", version: "v3", publishedAt: "2026-11-01 08:00", changeNote: "定期更新", changeNoteKey: "admin.changeNoteUpdated", effectiveDate: "2026-11-01" },
  { id: "privacy-v2-history", agreementId: "privacy-v3", version: "v2", publishedAt: "2026-09-01 08:00", changeNote: "初始版本", changeNoteKey: "admin.changeNoteInitial", effectiveDate: "2026-09-01" }
];
const integrations: IntegrationRecord[] = [
  { id: "maps", name: "Maps", environment: "test", endpoint: "https://maps.example.invalid", secretLast4: "4821", enabled: false },
  { id: "telegram", name: "Telegram", environment: "test", endpoint: "https://telegram.example.invalid", secretLast4: "1190", lastTest: { ok: true, latencyMs: 96, testedAt: "2026-11-14 05:00" }, enabled: true }
];
const calls: IntegrationCallRecord[] = [{ id: "call-1", integrationId: "telegram", occurredAt: "2026-11-14 05:00", ok: true, latencyMs: 96 }];
const wait = () => new Promise((resolve) => setTimeout(resolve, 120));
export const mockApi: AdminApi = {
  async login(input) { await wait(); if (!input.email || !input.password || !/^[0-9]{6,8}$/.test(input.totpCode)) throw Object.assign(new Error("login_failed"), { status: 401 }); mockLoggedIn = true; return {}; },
  async refresh() { await wait(); if (!mockLoggedIn) throw Object.assign(new Error("refresh_failed"), { status: 401 }); },
  async logout() { await wait(); mockLoggedIn = false; },
  async changePassword() { await wait(); },
  async stepUp() { await wait(); return "mock-step-up"; },
  async listAdmins() { await wait(); return { items: structuredClone(mockAdmins) }; },
  async createAdmin(input) { await wait(); const admin: AdminUserRecord = { publicId: `mock-${mockAdmins.length + 1}`, name: input.name, email: input.email, role: input.role, dataScope: input.dataScope, status: "active", createdAt: new Date().toISOString() }; mockAdmins.push(admin); return { admin: structuredClone(admin), initialPassword: "mock-initial-password-123456", totpProvisioningUri: "otpauth://totp/Dr.life%2B:admin%40example.test?secret=MOCKONLY" }; },
  async disableAdmin(id) { await wait(); const admin = mockAdmins.find((item) => item.publicId === id); if (!admin) throw new Error("not_found"); admin.status = "disabled"; return structuredClone(admin); },
  async enableAdmin(id) { await wait(); const admin = mockAdmins.find((item) => item.publicId === id); if (!admin) throw new Error("not_found"); admin.status = "active"; return structuredClone(admin); },
  async updateAdminRole(id, role, dataScope) { await wait(); const admin = mockAdmins.find((item) => item.publicId === id); if (!admin) throw new Error("not_found"); admin.role = role; admin.dataScope = dataScope; return structuredClone(admin); },
  async listAudit() { await wait(); return { items: structuredClone(mockAudit) }; },
  async listReviews() { await wait(); return structuredClone(reviews); },
  async approveReview(kind, id) { await wait(); const item = reviews[kind === "merchant" ? "merchants" : "products"].find((entry) => entry.id === id); if (item) item.status = "approved"; },
  async rejectReview(kind, id) { await wait(); const item = reviews[kind === "merchant" ? "merchants" : "products"].find((entry) => entry.id === id); if (item) item.status = "rejected"; },
  async listOrders(filters = {}) { await wait(); return orders.filter((item) => (!filters.status || item.status === filters.status) && (!filters.query || (item.id + " " + item.buyerKey + " " + item.merchantKey).toLowerCase().includes(filters.query.toLowerCase())) && (!filters.from || item.createdAt.slice(0, 10) >= filters.from) && (!filters.to || item.createdAt.slice(0, 10) <= filters.to)); },
  async getOrder(id) { await wait(); return orders.find((item) => item.id === id); },
  async listAgreements() { await wait(); return structuredClone(agreements); },
  async listAgreementVersions(id) { await wait(); return structuredClone(agreementVersions.filter((entry) => entry.agreementId === id)); },
  async saveAgreementDraft(id, language, value) { await wait(); const item = agreements.find((entry) => entry.id === id); if (!item) throw new Error("agreement_not_found"); item.languages[language] = value; return structuredClone(item); },
  async publishAgreement(id, input) { await wait(); const item = agreements.find((entry) => entry.id === id); if (!item) throw new Error("agreement_not_found"); if (!input.changeNote || !input.effectiveDate || Object.values(item.languages).some((value) => !value.trim())) throw new Error("agreement_incomplete"); item.state = "published"; item.effectiveDate = input.effectiveDate; item.version = `v${Number(item.version.slice(1)) + 1}`; return structuredClone(item); },
  async listIntegrations() { await wait(); return structuredClone(integrations); },
  async updateIntegration(id, input) { await wait(); const item = integrations.find((entry) => entry.id === id); if (!item) throw new Error("integration_not_found"); item.endpoint = input.endpoint; item.environment = input.environment; if (input.secret) item.secretLast4 = input.secret.slice(-4); item.lastTest = undefined; item.enabled = false; return structuredClone(item); },
  async testIntegration(id) { await wait(); const item = integrations.find((entry) => entry.id === id); if (!item) throw new Error("integration_not_found"); item.lastTest = { ok: true, latencyMs: 80 + id.length, testedAt: "2026-11-14 05:00" }; return item.lastTest; },
  async enableIntegration(id) { await wait(); const item = integrations.find((entry) => entry.id === id); if (!item || !item.lastTest?.ok) throw new Error("integration_not_tested"); item.enabled = true; return structuredClone(item); },
  async listIntegrationCalls(id) { await wait(); return structuredClone(calls.filter((entry) => entry.integrationId === id)); }
};
