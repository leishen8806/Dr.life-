import en from "./en.json";
import km from "./km.json";
import { pageCopy } from "./page-copy";
import zh from "./zh.json";

export const locales = {
  zh: { ...zh, home: { ...zh.home, ...pageCopy.zh.home }, merchant: { ...zh.merchant, ...pageCopy.zh.merchant }, legal: { ...zh.legal, ...pageCopy.zh.legal } },
  en: { ...en, home: { ...en.home, ...pageCopy.en.home }, merchant: { ...en.merchant, ...pageCopy.en.merchant }, legal: { ...en.legal, ...pageCopy.en.legal } },
  km: { ...km, home: { ...km.home, ...pageCopy.km.home }, merchant: { ...km.merchant, ...pageCopy.km.merchant }, legal: { ...km.legal, ...pageCopy.km.legal } }
} as const;
export type Locale = keyof typeof locales;
export const localeCodes = Object.keys(locales) as Locale[];

export function getLocale(value: string | null | undefined): Locale {
  return value && localeCodes.includes(value as Locale) ? (value as Locale) : "zh";
}

export function read(object: unknown, path: string): string {
  const value = path.split(".").reduce<unknown>((current, key) => {
    return current && typeof current === "object" ? (current as Record<string, unknown>)[key] : undefined;
  }, object);
  return typeof value === "string" ? value : "";
}
