import { describe, expect, it } from "vitest";
import { ApiError, CircuitOpenError, type ApiErrorCode } from "@drlife/api-client";
import { productErrorKey } from "./errors";
import { productActions, statusLabelKey, statusTone } from "./status";
import type { ProductStatus } from "./status";

const statuses: ProductStatus[] = ["draft", "review", "on", "off", "rejected"];

describe("status labels", () => {
  it("labels all five product statuses through the dictionary", () => {
    expect(statuses.map(statusLabelKey)).toEqual([
      "product.status.draft",
      "product.status.review",
      "product.status.on",
      "product.status.off",
      "product.status.rejected"
    ]);
  });

  it("gives each status a tone the shared StatusTag understands", () => {
    expect(statuses.map(statusTone)).toEqual([
      "neutral",
      "warning",
      "success",
      "neutral",
      "danger"
    ]);
  });
});

describe("productActions", () => {
  it("offers listing only from the statuses the backend accepts", () => {
    // catalog.ValidateProductForPublish rejects a product that is already on sale.
    expect(productActions("draft").canPublish).toBe(true);
    expect(productActions("off").canPublish).toBe(true);
    expect(productActions("rejected").canPublish).toBe(true);
    expect(productActions("on").canPublish).toBe(false);
    expect(productActions("review").canPublish).toBe(false);
  });

  it("offers taking off sale only while the product is on sale", () => {
    expect(productActions("on").canUnpublish).toBe(true);
    for (const status of ["draft", "review", "off", "rejected"] as const) {
      expect(productActions(status).canUnpublish).toBe(false);
    }
  });

  it("locks editing while the platform is reviewing the product", () => {
    expect(productActions("review").canEdit).toBe(false);
    for (const status of ["draft", "on", "off", "rejected"] as const) {
      expect(productActions(status).canEdit).toBe(true);
    }
  });
});

describe("productErrorKey", () => {
  const apiError = (code: number) =>
    new ApiError(code as ApiErrorCode, "req_01HZY0", 400);

  it("explains a rejected listing instead of showing the generic 1002 text", () => {
    expect(productErrorKey(apiError(1002), "publish")).toBe(
      "product.error.publishBlocked"
    );
  });

  it("falls back to the shared error copy for every other action", () => {
    expect(productErrorKey(apiError(1002), "save")).toBe("error.1002");
    expect(productErrorKey(apiError(1002), "load")).toBe("error.1002");
  });

  it("maps the documented application error codes onto their dictionary keys", () => {
    expect(productErrorKey(apiError(1003), "save")).toBe("error.1003");
    expect(productErrorKey(apiError(1004), "load")).toBe("error.1004");
    expect(productErrorKey(apiError(1009), "save")).toBe("error.1009");
    expect(productErrorKey(apiError(1010), "publish")).toBe("error.1010");
    expect(productErrorKey(apiError(2001), "load")).toBe("error.2001");
    expect(productErrorKey(apiError(2002), "unpublish")).toBe("error.2002");
    expect(productErrorKey(apiError(7001), "load")).toBe("error.7001");
  });

  it("degrades an undocumented code to the generic internal error", () => {
    expect(productErrorKey(apiError(4242), "save")).toBe("error.1001");
  });

  it("maps an open circuit and a transport failure onto their own copy", () => {
    expect(productErrorKey(new CircuitOpenError("merchant"), "load")).toBe(
      "error.circuitOpen"
    );
    expect(productErrorKey(new TypeError("Failed to fetch"), "load")).toBe(
      "common.networkError"
    );
    expect(productErrorKey(undefined, "load")).toBe("common.networkError");
  });
});
