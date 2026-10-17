import type { components } from "./schema";

export type ApiErrorCode = components["schemas"]["ErrorCode"];
export type ApiErrorKind = "internal" | "validation" | "notFound" | "rateLimited" | "unavailable" | "unsupported" | "idempotencyConflict" | "idempotencyPending" | "alreadyProcessed" | "unauthenticated" | "forbidden" | "stepUp" | "external" | "unknown";

const kinds: Partial<Record<ApiErrorCode, ApiErrorKind>> = {
  1001: "internal", 1002: "validation", 1003: "notFound", 1004: "rateLimited", 1005: "unavailable", 1006: "unsupported", 1007: "validation", 1008: "unsupported",
  1009: "idempotencyConflict", 1010: "idempotencyPending", 1011: "alreadyProcessed", 2001: "unauthenticated", 2002: "forbidden", 2003: "stepUp", 2004: "stepUp", 7001: "external"
};

export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  readonly messageKey: `error.${number}`;
  constructor(
    public readonly code: ApiErrorCode,
    public readonly requestId: string,
    public readonly status: number
  ) {
    super("Request failed");
    this.name = "ApiError";
    this.kind = kinds[code] ?? "unknown";
    this.messageKey = `error.${code}`;
  }
}
