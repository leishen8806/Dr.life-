export interface MerchantCompanyInput { name: string; address: string; category: string; employees: number; stores: number; city: string; }
export interface ProductImportInput { source: "1688" | "amazon" | "shopee" | "csv"; rightsConfirmed: boolean; }
export interface MerchantNotificationSetting { event: "order" | "wallet" | "product" | "warehouse"; inApp: boolean; telegram: boolean; }
export interface OnboardingInput { companyName: string; contact: string; phone: string; licenseFileName: string; category: string; }
export interface MerchantV7Api { updateCompany(input: MerchantCompanyInput): Promise<void>; importProducts(input: ProductImportInput): Promise<{ status: "pending_review" }>; updateNotificationSettings(input: MerchantNotificationSetting[]): Promise<void>; submitOnboarding(input: OnboardingInput): Promise<{ status: "pending_review" }>; }

export function createMerchantV7Api(): MerchantV7Api {
  const unsupported = () => { throw new Error("backend_field_mapping_pending"); };
  if (!import.meta.env.DEV) return { updateCompany: unsupported, importProducts: unsupported, updateNotificationSettings: unsupported, submitOnboarding: unsupported };
  return { async updateCompany() {}, async importProducts() { return { status: "pending_review" }; }, async updateNotificationSettings() {}, async submitOnboarding() { return { status: "pending_review" }; } };
}
