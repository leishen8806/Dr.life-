import { ApiError, CircuitOpenError } from "@drlife/api-client";
import type { TranslationKey } from "@drlife/i18n";

export type ProductAction = "load" | "save" | "publish" | "unpublish";

/**
 * 只登记有词条的应用错误码。值类型是 TranslationKey，
 * 所以忘了加词条会在 typecheck 就拦住，而不是上线后显示空白。
 * 服务端返回的 message 不直接显示：文案一律走 packages/i18n。
 */
const codeKeys: Record<number, TranslationKey> = {
  1001: "error.1001",
  1002: "error.1002",
  1003: "error.1003",
  1004: "error.1004",
  1005: "error.1005",
  1006: "error.1006",
  1007: "error.1007",
  1008: "error.1008",
  1009: "error.1009",
  1010: "error.1010",
  1011: "error.1011",
  2001: "error.2001",
  2002: "error.2002",
  2003: "error.2003",
  2004: "error.2004",
  2005: "error.2005",
  2006: "error.2006",
  2007: "error.2007",
  7001: "error.7001"
};

export function productErrorKey(
  error: unknown,
  action: ProductAction
): TranslationKey {
  if (error instanceof CircuitOpenError) return "error.circuitOpen";
  if (!(error instanceof ApiError)) return "common.networkError";
  // 上架被拒基本都是三语或阶梯价没齐，通用的 1002 文案说不清要补什么。
  if (error.code === 1002 && action === "publish") {
    return "product.error.publishBlocked";
  }
  return codeKeys[error.code] ?? "error.1001";
}
