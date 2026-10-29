import type { Language } from "@drlife/i18n";
import type { LocalizedText } from "./types";

/**
 * Catalogue text is trilingual. The API requires all three languages, so the
 * fallbacks only guard against a blank value reaching the screen.
 */
export function localized(
  text: LocalizedText | undefined,
  language: Language
): string {
  if (!text) return "";
  return text[language].trim() || text.en.trim() || text.zh.trim();
}
