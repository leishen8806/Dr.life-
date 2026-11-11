import type { Language } from "@drlife/i18n";

export function formatOrderDateTime(value: string, language: Language): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(language, {
    dateStyle: "short",
    timeStyle: "short",
    timeZone: "Asia/Phnom_Penh"
  }).format(date);
}
