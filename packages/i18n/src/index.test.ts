import { describe, expect, it } from "vitest";
import {
  applyDocumentLanguage,
  plural,
  readLanguagePreference,
  resolveLanguage,
  storeLanguagePreference,
  t
} from "./index";

describe("i18n", () => {
  it("uses account language before Telegram language", () => {
    expect(resolveLanguage("zh", "km")).toBe("zh");
    expect(resolveLanguage("zh-CN", "en")).toBe("zh");
    expect(resolveLanguage("zh_TW", "en")).toBe("zh");
    expect(resolveLanguage(undefined, "km-KH")).toBe("km");
    expect(resolveLanguage(undefined, "EN-us")).toBe("en");
    expect(resolveLanguage("", "km-KH")).toBe("km");
    expect(resolveLanguage(undefined, "fr")).toBe("en");
    expect(resolveLanguage("unknown", "unknown")).toBe("en");
  });

  it("interpolates named placeholders", () => {
    expect(t("shell.currentLanguage", { language: "English" }, "en")).toBe(
      "Current language: English"
    );
  });

  it("provides the shared brand slogan in every supported language", () => {
    for (const language of ["zh", "en", "km"] as const) {
      expect(t("brand.slogan", {}, language)).not.toBe("");
    }
  });

  it("selects explicit singular and plural keys", () => {
    expect(plural(1, "order.count.one", "order.count.other")).toBe(
      "order.count.one"
    );
    expect(plural(0, "order.count.one", "order.count.other")).toBe(
      "order.count.other"
    );
    expect(plural(2, "order.count.one", "order.count.other")).toBe(
      "order.count.other"
    );
  });

  it("reads and writes local language preferences without requiring storage", () => {
    const values = new Map<string, string>();
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value)
    };

    storeLanguagePreference("km", storage);
    expect(readLanguagePreference(storage)).toBe("km");
    values.set("drlife_lang", "unsupported");
    expect(readLanguagePreference(storage)).toBeUndefined();
  });

  it("updates the root language", () => {
    const root = { lang: "en" };
    applyDocumentLanguage("zh", root);
    expect(root.lang).toBe("zh");
  });
});
