export interface BuyerInquiryInput { companyId: string; description: string; budget: string; duration: string; region: string; contact: "telegram" | "phone" | "email"; }
export interface ReceiptConfirmation { orderId: string; proof: "photo" | "signature" | "code"; value?: string; }
export interface ProblemReport { orderId: string; type: string; description: string; imageCount: number; }
export interface BuyerNotification { id: string; event: "order" | "wallet" | "product" | "warehouse"; read: boolean; target: string; }
export interface BuyerV7Api { submitInquiry(input: BuyerInquiryInput): Promise<void>; confirmReceipt(input: ReceiptConfirmation): Promise<void>; reportProblem(input: ProblemReport): Promise<void>; listNotifications(): Promise<BuyerNotification[]>; markNotificationsRead(ids?: string[]): Promise<void>; }

export function createBuyerV7Api(): BuyerV7Api {
  const unsupported = () => { throw new Error("backend_field_mapping_pending"); };
  if (!import.meta.env.DEV) return { submitInquiry: unsupported, confirmReceipt: unsupported, reportProblem: unsupported, listNotifications: async () => [], markNotificationsRead: unsupported };
  return { async submitInquiry() {}, async confirmReceipt() {}, async reportProblem() {}, async listNotifications() { return [{ id: "n1", event: "order", read: false, target: "order" }]; }, async markNotificationsRead() {} };
}
