// @vitest-environment jsdom
/**
 * `LanguageSwitcherView` is covered by the markup tests next door, which keep
 * running under the default node environment. What only a DOM can show is what
 * the connected switcher does besides re-rendering: the page language and the
 * stored preference the next visit reads.
 */
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { I18nProvider, readLanguagePreference, t } from "@drlife/i18n";
import { LanguageSwitcher } from "./language-switcher";

afterEach(cleanup);

describe("LanguageSwitcher", () => {
  it("carries the choice to the document and to the stored preference", () => {
    render(
      <I18nProvider initialLanguage="en">
        <LanguageSwitcher />
      </I18nProvider>
    );
    const khmer = screen.getByRole("button", { name: t("language.km") });
    expect(khmer.getAttribute("aria-pressed")).toBe("false");

    fireEvent.click(khmer);

    expect(document.documentElement.lang).toBe("km");
    expect(readLanguagePreference()).toBe("km");
    expect(khmer.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: t("language.en") }).getAttribute("aria-pressed")).toBe("false");
  });
});
