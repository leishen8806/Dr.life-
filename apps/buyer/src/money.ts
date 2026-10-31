import type { ProductTier } from "./types";

const scale = 10_000n;

/**
 * Prices arrive as integers in ten-thousandths of a US dollar. The conversion
 * stays in bigint so a large amount can never lose a cent to a float.
 */
export function formatPriceE4(valueE4: number | bigint): string {
  const amount = BigInt(valueE4);
  const negative = amount < 0n;
  const absolute = negative ? -amount : amount;
  const fraction = (absolute % scale).toString().padStart(4, "0").replace(/0+$/, "");
  const decimals = fraction.length < 2 ? fraction.padEnd(2, "0") : fraction;
  return `${negative ? "-" : ""}$${absolute / scale}.${decimals}`;
}

function parts(quantity: string): { whole: string; fraction: string } {
  const [whole = "0", fraction = ""] = quantity.trim().split(".");
  return { whole: whole.replace(/^0+(?=\d)/, ""), fraction };
}

/** Minimum quantities are decimal strings, so they are compared digit by digit. */
export function compareQuantity(a: string, b: string): number {
  const left = parts(a);
  const right = parts(b);
  if (left.whole.length !== right.whole.length)
    return left.whole.length - right.whole.length;
  if (left.whole !== right.whole) return left.whole < right.whole ? -1 : 1;
  const width = Math.max(left.fraction.length, right.fraction.length);
  const leftFraction = left.fraction.padEnd(width, "0");
  const rightFraction = right.fraction.padEnd(width, "0");
  if (leftFraction === rightFraction) return 0;
  return leftFraction < rightFraction ? -1 : 1;
}

export function sortedTiers(tiers: readonly ProductTier[]): ProductTier[] {
  return [...tiers].sort((a, b) => compareQuantity(a.min_qty, b.min_qty));
}

/** The tier a buyer reaches first: the lowest minimum quantity on offer. */
export function entryTier(tiers: readonly ProductTier[]): ProductTier | undefined {
  return sortedTiers(tiers)[0];
}
