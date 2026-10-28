import { describe, expect, it } from "vitest";
import { ApiError } from "./errors";

describe("ApiError", () => {
  it("maps documented codes without putting server text in the display message", () => {
    const error = new ApiError(1009, "request_1", 422);

    expect(error.kind).toBe("idempotencyConflict");
    expect(error.messageKey).toBe("error.1009");
    expect(error.message).not.toContain("internal details");
  });

  it("classifies request-too-large and unsupported-media errors", () => {
    expect(new ApiError(1007, "request_1", 413).kind).toBe("validation");
    expect(new ApiError(1008, "request_1", 415).kind).toBe("unsupported");
  });

  it("classifies the login hardening codes instead of falling back to unknown", () => {
    expect(new ApiError(2005, "request_1", 401).kind).toBe("loginFailed");
    expect(new ApiError(2006, "request_1", 403).kind).toBe("accountBlocked");
    expect(new ApiError(2007, "request_1", 429).kind).toBe("loginThrottled");
  });

  it("points the login hardening codes at their own message keys", () => {
    expect(new ApiError(2005, "request_1", 401).messageKey).toBe("error.2005");
    expect(new ApiError(2006, "request_1", 403).messageKey).toBe("error.2006");
    expect(new ApiError(2007, "request_1", 429).messageKey).toBe("error.2007");
  });

  it("reads the lockout window from a delta-seconds Retry-After", () => {
    expect(new ApiError(2007, "request_1", 429, { retryAfter: "45" }).retryAfterSeconds).toBe(45);
    expect(new ApiError(2007, "request_1", 429, { retryAfter: " 45 " }).retryAfterSeconds).toBe(45);
    expect(new ApiError(2007, "request_1", 429, { retryAfter: "0" }).retryAfterSeconds).toBe(0);
  });

  it("reads the lockout window from an HTTP-date Retry-After and rounds up to whole seconds", () => {
    const now = Date.UTC(2026, 9, 28, 12, 0, 0);
    expect(
      new ApiError(2007, "request_1", 429, { retryAfter: "Wed, 28 Oct 2026 12:00:30 GMT", now }).retryAfterSeconds
    ).toBe(30);
    expect(
      new ApiError(2007, "request_1", 429, { retryAfter: "Wed, 28 Oct 2026 12:00:30 GMT", now: now + 29_500 })
        .retryAfterSeconds
    ).toBe(1);
    expect(
      new ApiError(2007, "request_1", 429, { retryAfter: "Wed, 28 Oct 2026 11:59:00 GMT", now }).retryAfterSeconds
    ).toBe(0);
  });

  it("leaves the lockout window undefined when the header is missing or unusable", () => {
    expect(new ApiError(2007, "request_1", 429).retryAfterSeconds).toBeUndefined();
    expect(new ApiError(2007, "request_1", 429, { retryAfter: null }).retryAfterSeconds).toBeUndefined();
    expect(new ApiError(2007, "request_1", 429, { retryAfter: "" }).retryAfterSeconds).toBeUndefined();
    expect(new ApiError(2007, "request_1", 429, { retryAfter: "soon" }).retryAfterSeconds).toBeUndefined();
    expect(new ApiError(2007, "request_1", 429, { retryAfter: "-5" }).retryAfterSeconds).toBeUndefined();
  });

  it("never hands a blocked account a retry countdown", () => {
    const error = new ApiError(2006, "request_1", 403, { retryAfter: "60" });

    expect(error.kind).toBe("accountBlocked");
    expect(error.retryAfterSeconds).toBeUndefined();
  });
});
