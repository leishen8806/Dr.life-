import { describe, expect, it } from "vitest";
import { ApiError, CircuitOpenError } from "@drlife/api-client";
import { compareQuantity, entryTier, formatPriceE4 } from "./money";
import { localized } from "./localized";
import { errorMessageKey } from "./resource";
import type { ProductTier } from "./types";

function tier(min_qty: string, unit_price_e4: number): ProductTier {
  return { min_qty, unit_price_e4 };
}

describe("formatPriceE4", () => {
  it("renders ten-thousandths of a dollar with at least two decimals", () => {
    expect(formatPriceE4(0)).toBe("$0.00");
    expect(formatPriceE4(95_000)).toBe("$9.50");
    expect(formatPriceE4(1_800_000)).toBe("$180.00");
    expect(formatPriceE4(46_000)).toBe("$4.60");
  });

  it("keeps the fourth decimal when the price needs it", () => {
    expect(formatPriceE4(1)).toBe("$0.0001");
    expect(formatPriceE4(123_456)).toBe("$12.3456");
  });

  it("stays exact beyond the safe integer range, so no float is involved", () => {
    expect(formatPriceE4(90_071_992_547_409_910n)).toBe("$9007199254740.991");
  });

  it("marks a negative amount before the currency symbol", () => {
    expect(formatPriceE4(-5_000)).toBe("-$0.50");
  });
});

describe("compareQuantity", () => {
  it("orders decimal strings numerically, not lexically", () => {
    expect(compareQuantity("100", "20")).toBeGreaterThan(0);
    expect(compareQuantity("2.5", "10")).toBeLessThan(0);
    expect(compareQuantity("1", "1")).toBe(0);
    expect(compareQuantity("0.5", "0.50")).toBe(0);
    expect(compareQuantity("0.05", "0.5")).toBeLessThan(0);
  });
});

describe("entryTier", () => {
  it("picks the tier with the smallest minimum quantity regardless of input order", () => {
    const tiers = [tier("500", 82_000), tier("1", 95_000), tier("100", 88_000)];
    expect(entryTier(tiers)).toEqual(tier("1", 95_000));
  });

  it("returns undefined when a product carries no tier", () => {
    expect(entryTier([])).toBeUndefined();
  });
});

describe("localized", () => {
  it("returns the text for the active language", () => {
    const text = { zh: "瓷砖", en: "Tile", km: "ក្បឿង" };
    expect(localized(text, "zh")).toBe("瓷砖");
    expect(localized(text, "en")).toBe("Tile");
    expect(localized(text, "km")).toBe("ក្បឿង");
  });

  it("falls back without showing an empty label", () => {
    expect(localized({ zh: "瓷砖", en: "Tile", km: "  " }, "km")).toBe("Tile");
    expect(localized({ zh: "瓷砖", en: "", km: "" }, "en")).toBe("瓷砖");
    expect(localized(undefined, "en")).toBe("");
  });
});

describe("errorMessageKey", () => {
  it("maps a server error code to its translated message key", () => {
    expect(errorMessageKey(new ApiError(1004, "req_1", 429))).toBe("error.1004");
    expect(errorMessageKey(new ApiError(1002, "req_2", 400))).toBe("error.1002");
    expect(errorMessageKey(new ApiError(7001, "req_3", 502))).toBe("error.7001");
  });

  it("maps an open circuit to its own message key", () => {
    expect(errorMessageKey(new CircuitOpenError("public"))).toBe(
      "error.circuitOpen"
    );
  });

  it("falls back to a network error for anything else", () => {
    expect(errorMessageKey(new TypeError("fetch failed"))).toBe(
      "common.networkError"
    );
    expect(errorMessageKey(undefined)).toBe("common.networkError");
  });
});
