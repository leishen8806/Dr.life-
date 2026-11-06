import type { Product, ProductTier } from "./types";
import { compareQuantity, sortedTiers } from "./money";

const quantityScale = 10_000n;

export type CartError = "belowMoq" | "overAvailable" | "invalidQuantity";

export interface CartItem {
  product: Product;
  quantity: string;
  availableQuantity: string;
  merchantKey: "china" | "local";
}

export interface CartUpdate {
  cart: CartItem[];
  error?: CartError;
}

function quantityUnits(value: string): bigint | undefined {
  const normalized = value.trim();
  if (!/^\d+(?:\.\d{1,4})?$/.test(normalized)) return undefined;
  const [whole, fraction = ""] = normalized.split(".");
  return BigInt(whole ?? "0") * quantityScale + BigInt(fraction.padEnd(4, "0"));
}

function displayQuantity(units: bigint): string {
  const whole = units / quantityScale;
  const fraction = (units % quantityScale).toString().padStart(4, "0").replace(/0+$/, "");
  return fraction ? `${whole}.${fraction}` : whole.toString();
}

export function tierForQuantity(
  tiers: readonly ProductTier[],
  quantity: string
): ProductTier | undefined {
  return sortedTiers(tiers).reduce<ProductTier | undefined>(
    (current, tier) => (compareQuantity(tier.min_qty, quantity) <= 0 ? tier : current),
    undefined
  );
}

export function lineUnitPrice(item: CartItem): number {
  return tierForQuantity(item.product.tiers, item.quantity)?.unit_price_e4 ?? 0;
}

/** Money remains e4 bigint; fractional quantities are converted without floating point. */
export function lineTotalE4(item: CartItem): bigint {
  const units = quantityUnits(item.quantity);
  if (units === undefined) return 0n;
  return (BigInt(lineUnitPrice(item)) * units) / quantityScale;
}

export function cartTotalE4(cart: readonly CartItem[]): bigint {
  return cart.reduce((total, item) => total + lineTotalE4(item), 0n);
}

export function cartCount(cart: readonly CartItem[]): string {
  return displayQuantity(cart.reduce((total, item) => total + (quantityUnits(item.quantity) ?? 0n), 0n));
}

export function addProduct(
  cart: readonly CartItem[],
  product: Product,
  availableQuantity = "100"
): CartUpdate {
  const minimum = sortedTiers(product.tiers)[0]?.min_qty ?? "0";
  const existing = cart.find((item) => item.product.public_id === product.public_id);
  if (!existing) {
    const minimumUnits = quantityUnits(minimum);
    const availableUnits = quantityUnits(availableQuantity);
    if (minimumUnits === undefined) return { cart: [...cart], error: "invalidQuantity" };
    if (availableUnits !== undefined && minimumUnits > availableUnits)
      return { cart: [...cart], error: "overAvailable" };
    return {
      cart: [
        ...cart,
        {
          product,
          quantity: minimum,
          availableQuantity,
          merchantKey: product.origin === "cn" ? "china" : "local"
        }
      ]
    };
  }
  const nextQuantity = displayQuantity((quantityUnits(existing.quantity) ?? 0n) + (quantityUnits(minimum) ?? 0n));
  return updateQuantity(
    cart,
    product.public_id,
    nextQuantity,
    existing?.availableQuantity ?? availableQuantity,
    existing?.merchantKey ?? (product.origin === "cn" ? "china" : "local")
  );
}

export function updateQuantity(
  cart: readonly CartItem[],
  publicId: string,
  quantity: string,
  availableQuantity?: string,
  merchantKey?: CartItem["merchantKey"]
): CartUpdate {
  const item = cart.find((entry) => entry.product.public_id === publicId);
  if (!item) return { cart: [...cart], error: "invalidQuantity" };
  const next = quantityUnits(quantity);
  if (next === undefined) return { cart: [...cart], error: "invalidQuantity" };
  if (next === 0n) return { cart: cart.filter((entry) => entry.product.public_id !== publicId) };
  const maximum = quantityUnits(availableQuantity ?? item.availableQuantity);
  if (maximum !== undefined && next > maximum) return { cart: [...cart], error: "overAvailable" };
  const minimum = sortedTiers(item.product.tiers)[0]?.min_qty ?? "0";
  if (compareQuantity(quantity, minimum) < 0) return { cart: [...cart], error: "belowMoq" };
  return {
    cart: cart.map((entry) =>
      entry.product.public_id === publicId
        ? {
            ...entry,
            quantity: displayQuantity(next),
            availableQuantity: availableQuantity ?? entry.availableQuantity,
            merchantKey: merchantKey ?? entry.merchantKey
          }
        : entry
    )
  };
}

export function stepQuantity(
  cart: readonly CartItem[],
  publicId: string,
  delta: -1 | 1
): CartUpdate {
  const item = cart.find((entry) => entry.product.public_id === publicId);
  const current = item && quantityUnits(item.quantity);
  if (!item || current === undefined) return { cart: [...cart], error: "invalidQuantity" };
  return updateQuantity(
    cart,
    publicId,
    displayQuantity(
      current + BigInt(delta) * quantityScale < 0n
        ? 0n
        : current + BigInt(delta) * quantityScale
    )
  );
}
