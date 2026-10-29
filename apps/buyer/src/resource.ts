import { ApiError, CircuitOpenError, type components } from "@drlife/api-client";
import type { TranslationKey } from "@drlife/i18n";

/** Every list and detail panel is in exactly one of these three states. */
export type Resource<T> =
  | { status: "loading" }
  | { status: "ready"; value: T }
  | { status: "error"; messageKey: TranslationKey };

/**
 * The dictionary carries one message per server error code. Listing them makes
 * the compiler complain if the schema gains a code without a translation.
 */
const serverMessageKeys = {
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
} satisfies Record<components["schemas"]["ErrorCode"], TranslationKey>;

/** A deployed server can answer with a code newer than the generated schema. */
const knownMessageKeys = serverMessageKeys as Record<
  number,
  TranslationKey | undefined
>;

export function errorMessageKey(error: unknown): TranslationKey {
  if (error instanceof ApiError)
    return knownMessageKeys[error.code] ?? "error.1001";
  if (error instanceof CircuitOpenError) return "error.circuitOpen";
  return "common.networkError";
}
