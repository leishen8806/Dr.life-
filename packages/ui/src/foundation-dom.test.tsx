// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Money, ProductCard, Tabs, TieredPrice, Toast } from "./index";

const moneyProps = { invalidLabel: "Invalid amount", currencySymbol: "$", language: "en" as const };

describe("foundation DOM acceptance", () => {
  it("renders the required e4 Money examples and invalid states", () => {
    const cases = [["0", "$0.00"], ["100", "$0.01"], ["12500", "$1.25"], ["10000", "$1.00"], ["123456700", "$12,345.67"]] as const;
    for (const [value, expected] of cases) {
      const view = render(<Money value={value} {...moneyProps} />);
      expect(view.container.textContent).toBe(expected);
      view.unmount();
    }
    for (const value of ["12501", "-10000", "", "abc"]) {
      const invalid = render(<Money value={value} {...moneyProps} />);
      expect(invalid.container.querySelector('[role="alert"]')?.textContent).toBe("Invalid amount");
      expect(invalid.container.querySelector("[data-e4]")).toBeNull();
      invalid.unmount();
    }
    const tiered = render(<TieredPrice tiers={[{ label: "1–9", value: "12500" }]} {...moneyProps} />);
    expect(tiered.container.textContent).toContain("$1.25");
  });

  it("keeps tabs keyboard navigable and alerts out of aria-live", () => {
    const change = vi.fn();
    render(<Tabs tabs={[{ id: "a", label: "A" }, { id: "b", label: "B" }]} activeId="a" onChange={change} ariaLabel="Tabs" />);
    const tabs = screen.getAllByRole("tab");
    expect(tabs[0]?.getAttribute("tabindex")).toBe("0");
    expect(tabs[1]?.getAttribute("tabindex")).toBe("-1");
    fireEvent.keyDown(tabs[0]!, { key: "ArrowRight" });
    expect(change).toHaveBeenCalledWith("b");
    const toast = render(<Toast role="alert">Failed</Toast>);
    expect(toast.container.querySelector('[role="alert"]')?.getAttribute("aria-live")).toBeNull();
  });

  it("uses a semantic heading for product cards", () => {
    render(<ProductCard imageAlt="Tile" name="Long tile name" price="$1.25" />);
    expect(screen.getByRole("heading", { name: "Long tile name", level: 3 })).toBeDefined();
  });
});
