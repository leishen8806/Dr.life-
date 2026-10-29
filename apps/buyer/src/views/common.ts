import type { Language, TranslationKey } from "@drlife/i18n";

/**
 * Views receive the translator instead of calling the hook, so tests can render
 * the same markup in all three languages.
 */
export type Translate = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string;

export interface LocaleProps {
  t: Translate;
  language: Language;
}
