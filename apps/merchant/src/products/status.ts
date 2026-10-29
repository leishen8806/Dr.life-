import type { TranslationKey } from "@drlife/i18n";
import type { Product } from "./form";

export type ProductStatus = Product["status"];
export type StatusTone = "neutral" | "success" | "warning" | "danger";

export interface ProductActions {
  canEdit: boolean;
  canPublish: boolean;
  canUnpublish: boolean;
}

const labelKeys: Record<ProductStatus, TranslationKey> = {
  draft: "product.status.draft",
  review: "product.status.review",
  on: "product.status.on",
  off: "product.status.off",
  rejected: "product.status.rejected"
};

const tones: Record<ProductStatus, StatusTone> = {
  draft: "neutral",
  review: "warning",
  on: "success",
  off: "neutral",
  rejected: "danger"
};

/**
 * 闸门与 catalog.Service 对齐：已上架的不再给上架入口，下架只对在售商品开放。
 * 平台审核中（review）连编辑也收起来，免得和审核人同时改同一条记录。
 */
const actions: Record<ProductStatus, ProductActions> = {
  draft: { canEdit: true, canPublish: true, canUnpublish: false },
  review: { canEdit: false, canPublish: false, canUnpublish: false },
  on: { canEdit: true, canPublish: false, canUnpublish: true },
  off: { canEdit: true, canPublish: true, canUnpublish: false },
  rejected: { canEdit: true, canPublish: true, canUnpublish: false }
};

export function statusLabelKey(status: ProductStatus): TranslationKey {
  return labelKeys[status];
}

export function statusTone(status: ProductStatus): StatusTone {
  return tones[status];
}

export function productActions(status: ProductStatus): ProductActions {
  return actions[status];
}
