import { describe, expect, it } from "vitest";
import { parsePriceE4, validatePriceRange } from "./search";

describe("parsePriceE4", () => {
  it.each([["1", 10000], ["1.2", 12000], ["1.25", 12500]])("parses %s as e4", (input, expected) => {
    expect(parsePriceE4(input)).toEqual({ ok: true, value: expected });
  });

  it.each(["1.255", "1.001", "-1", "abc", "1."]) ("rejects %s", (input) => {
    expect(parsePriceE4(input)).toMatchObject({ ok: false, messageKey: "mall.searchPriceInvalid" });
  });
});

describe("validatePriceRange", () => {
  it("rejects a minimum above the maximum without rounding", () => {
    expect(validatePriceRange("2", "1.99")).toEqual({ messageKey: "mall.searchPriceRange" });
  });

  it("omits blank boundaries", () => {
    expect(validatePriceRange("", "1.25")).toEqual({ maxPriceE4: 12500 });
  });
});
