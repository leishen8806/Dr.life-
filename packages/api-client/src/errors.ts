import type { components } from "./schema";

export type ApiErrorCode = components["schemas"]["ErrorCode"];
export type ApiErrorKind = "internal" | "validation" | "notFound" | "rateLimited" | "unavailable" | "unsupported" | "idempotencyConflict" | "idempotencyPending" | "alreadyProcessed" | "unauthenticated" | "forbidden" | "stepUp" | "loginFailed" | "accountBlocked" | "loginThrottled" | "external" | "unknown";

const kinds: Partial<Record<ApiErrorCode, ApiErrorKind>> = {
  1001: "internal", 1002: "validation", 1003: "notFound", 1004: "rateLimited", 1005: "unavailable", 1006: "unsupported", 1007: "validation", 1008: "unsupported",
  1009: "idempotencyConflict", 1010: "idempotencyPending", 1011: "alreadyProcessed", 2001: "unauthenticated", 2002: "forbidden", 2003: "stepUp", 2004: "stepUp",
  2005: "loginFailed", 2006: "accountBlocked", 2007: "loginThrottled", 7001: "external"
};

export interface ApiErrorContext {
  /** Raw `Retry-After` response header; 2007 uses it to carry the login lockout window. */
  retryAfter?: string | null;
  now?: number;
}

/** Delta-seconds or HTTP-date, per RFC 9110; anything else means the server gave us no usable window. */
function waitSeconds(retryAfter: string | null | undefined, now: number): number | undefined {
  const value = retryAfter?.trim();
  if (!value) return undefined;
  const seconds = Number(value);
  if (Number.isFinite(seconds)) return seconds >= 0 ? seconds : undefined;
  const date = Date.parse(value);
  return Number.isNaN(date) ? undefined : Math.max(0, Math.ceil((date - now) / 1_000));
}

export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  readonly messageKey: `error.${number}`;
  /** Seconds to wait before the request can succeed again, when the response said so. */
  readonly retryAfterSeconds: number | undefined;
  constructor(
    public readonly code: ApiErrorCode,
    public readonly requestId: string,
    public readonly status: number,
    context: ApiErrorContext = {}
  ) {
    super("Request failed");
    this.name = "ApiError";
    this.kind = kinds[code] ?? "unknown";
    this.messageKey = `error.${code}`;
    // A blocked account (2006) is not a timed lockout, so it never gets a countdown to retry against.
    this.retryAfterSeconds = this.kind === "accountBlocked" ? undefined : waitSeconds(context.retryAfter, context.now ?? Date.now());
  }
}
