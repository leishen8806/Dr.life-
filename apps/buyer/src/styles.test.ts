import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const styles = readFileSync(new URL("./styles.css", import.meta.url), "utf8");

function ruleFor(selector: string): string {
  const start = styles.indexOf(`${selector} {`);
  expect(start, `missing rule for ${selector}`).toBeGreaterThanOrEqual(0);
  return styles.slice(start, styles.indexOf("}", start));
}

describe("buyer mall styles", () => {
  it("never clips or truncates copy, so Khmer stays readable", () => {
    expect(styles).not.toMatch(/text-overflow/);
    expect(styles).not.toMatch(/white-space:\s*nowrap/);
    expect(styles).not.toMatch(/line-clamp/);
  });

  it("wraps long product and category labels instead of overflowing", () => {
    for (const selector of [
      ".buyer-product__name",
      ".buyer-product__spec",
      ".buyer-category",
      ".buyer-detail__name"
    ]) {
      expect(ruleFor(selector)).toContain("overflow-wrap: anywhere");
    }
  });

  it("lets the category grid and the sidebar grow with the label", () => {
    expect(ruleFor(".buyer-category-grid")).toContain("minmax(");
    expect(ruleFor(".buyer-category-rail")).toContain("min-width: 0");
  });
});
