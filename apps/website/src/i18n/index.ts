import en from "./en.json";
import km from "./km.json";
import zh from "./zh.json";

export const locales = { zh, en, km } as const;
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
