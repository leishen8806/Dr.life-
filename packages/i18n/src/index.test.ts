import { describe, expect, it } from "vitest";
import { resolveLanguage, t } from "./index";

describe("i18n", () => {
  it("uses account language before Telegram language", () => {
    expect(resolveLanguage("zh", "km")).toBe("zh");
    expect(resolveLanguage(undefined, "km-KH")).toBe("km");
    expect(resolveLanguage(undefined, "fr")).toBe("en");
  });

  it("interpolates named placeholders", () => {
    expect(t("shell.currentLanguage", { language: "English" }, "en")).toBe(
      "Current language: English"
    );
  });
});
