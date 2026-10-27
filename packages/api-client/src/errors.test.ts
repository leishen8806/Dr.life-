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
});
