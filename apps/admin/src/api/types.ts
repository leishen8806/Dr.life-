export type ReviewStatus = "pending" | "approved" | "rejected";
export type RecordStatus = "normal" | "attention" | "exception";

export interface ReviewRecord { id: string; nameKey: string; status: ReviewStatus; createdAt: string; }
export interface OrderRecord {
  id: string; buyerKey: string; merchantKey: string; amountE4: string; status: RecordStatus;
  createdAt: string; deliveryKey: string; phone: string;
  suborders: Array<{ id: string; productKey: string; status: RecordStatus }>;
}
export interface ReviewList { merchants: ReviewRecord[]; products: ReviewRecord[]; }

export interface AdminApi {
  listReviews(): Promise<ReviewList>;
  approveReview(kind: "merchant" | "product", id: string): Promise<void>;
  rejectReview(kind: "merchant" | "product", id: string, reason: string): Promise<void>;
  listOrders(filters?: { query?: string; status?: RecordStatus }): Promise<OrderRecord[]>;
  getOrder(id: string): Promise<OrderRecord | undefined>;
}
