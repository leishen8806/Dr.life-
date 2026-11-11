import type { TranslationKey } from "@drlife/i18n";

export type PriceParseResult =
  | { ok: true; value: number }
  | { ok: false; messageKey: TranslationKey };

/** Parse decimal dollars into integer e4 without floating point arithmetic. */
export function parsePriceE4(value: string): PriceParseResult {
  const text = value.trim();
  if (!/^\d+(?:\.\d{1,2})?$/.test(text)) {
    return { ok: false, messageKey: "mall.searchPriceInvalid" };
  }
  const [whole, fraction = ""] = text.split(".");
  const e4Text = `${whole}${fraction.padEnd(2, "0")}`;
  const e4 = Number.parseInt(e4Text, 10);
  if (!Number.isSafeInteger(e4)) {
    return { ok: false, messageKey: "mall.searchPriceInvalid" };
  }
  return { ok: true, value: e4 };
}

export function validatePriceRange(
  minimum: string,
  maximum: string
): { minPriceE4?: number; maxPriceE4?: number } | { messageKey: TranslationKey } {
  const min = minimum.trim() ? parsePriceE4(minimum) : undefined;
  const max = maximum.trim() ? parsePriceE4(maximum) : undefined;
  if (min && !min.ok) return min;
  if (max && !max.ok) return max;
  if (min && max && min.value > max.value) {
    return { messageKey: "mall.searchPriceRange" };
  }
  return {
    minPriceE4: min?.value,
    maxPriceE4: max?.value
  };
}
