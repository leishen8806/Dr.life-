export type ReviewStatus = "pending" | "approved" | "rejected";
export type RecordStatus = "normal" | "attention" | "exception";

export interface ReviewRecord { id: string; nameKey: string; status: ReviewStatus; createdAt: string; }
export interface OrderRecord {
  id: string; buyerKey: string; merchantKey: string; amountE4: string; status: RecordStatus;
  createdAt: string; deliveryKey: string; phone: string;
  suborders: Array<{ id: string; productKey: string; status: RecordStatus }>;
}
export interface ReviewList { merchants: ReviewRecord[]; products: ReviewRecord[]; }

export type AgreementKind = "privacy" | "terms";
export type AgreementState = "draft" | "published";
export interface AgreementRecord {
  id: string;
  kind: AgreementKind;
  version: string;
  effectiveDate: string;
  state: AgreementState;
  consented: number;
  required: number;
  languages: { zh: string; en: string; km: string };
}
export interface IntegrationRecord {
  id: string;
  name: string;
  environment: "test" | "production";
  endpoint: string;
  secretLast4: string;
  lastTest?: { ok: boolean; latencyMs: number; testedAt: string };
  enabled: boolean;
}
export interface IntegrationCallRecord {
  id: string;
  integrationId: string;
  occurredAt: string;
  ok: boolean;
  latencyMs: number;
}

export interface AdminApi {
  listReviews(): Promise<ReviewList>;
  approveReview(kind: "merchant" | "product", id: string): Promise<void>;
  rejectReview(kind: "merchant" | "product", id: string, reason: string): Promise<void>;
  listOrders(filters?: { query?: string; status?: RecordStatus; from?: string; to?: string }): Promise<OrderRecord[]>;
  getOrder(id: string): Promise<OrderRecord | undefined>;
  listAgreements(): Promise<AgreementRecord[]>;
  saveAgreementDraft(id: string, language: keyof AgreementRecord["languages"], value: string): Promise<AgreementRecord>;
  publishAgreement(id: string, input: { changeNote: string; effectiveDate: string }): Promise<AgreementRecord>;
  listIntegrations(): Promise<IntegrationRecord[]>;
  updateIntegration(id: string, input: { endpoint: string; environment: IntegrationRecord["environment"]; secret?: string }): Promise<IntegrationRecord>;
  testIntegration(id: string): Promise<IntegrationRecord["lastTest"]>;
  enableIntegration(id: string): Promise<IntegrationRecord>;
  listIntegrationCalls(id: string): Promise<IntegrationCallRecord[]>;
}
