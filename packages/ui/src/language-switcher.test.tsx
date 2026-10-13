import { Children, isValidElement, type ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import type { Language, TranslationKey } from "@drlife/i18n";
import { LanguageSwitcherView } from "./language-switcher";

describe("LanguageSwitcherView", () => {
  it("renders three translated choices and reports the selected language", () => {
    const onSelect = vi.fn();
    const view = LanguageSwitcherView({
      language: "en",
      translate: (key: TranslationKey) => key,
      onSelect
    });
    const buttons = Children.toArray(view.props.children).filter(
      isValidElement
    ) as ReactElement<{
      "aria-pressed": boolean;
      onClick: () => void;
      children: string;
    }>[];

    expect(buttons.map((button) => button.props.children)).toEqual([
      "language.zh",
      "language.en",
      "language.km"
    ]);
    expect(buttons[1]!.props["aria-pressed"]).toBe(true);
    buttons[2]!.props.onClick();
    expect(onSelect).toHaveBeenCalledWith("km" satisfies Language);
  });
});
