import { describe, expect, it } from "vitest";
import {
  addProduct,
  cartCount,
  cartTotalE4,
  lineTotalE4,
  tierForQuantity,
  updateQuantity
} from "./cart";
import type { Product } from "./types";

const product: Product = {
  public_id: "prd-cart",
  category_code: "c4",
  name: { zh: "瓷砖", en: "Tile", km: "ក្បឿង" },
  spec: undefined,
  unit: "m2",
  origin: "cn",
  lead_days: 10,
  status: "on",
  source: "manual",
  tiers: [
    { min_qty: "500", unit_price_e4: 82_000 },
    { min_qty: "1", unit_price_e4: 95_000 },
    { min_qty: "100", unit_price_e4: 88_000 }
  ],
  skus: [],
  updated_at: "2026-11-06T00:00:00Z"
};

describe("buyer cart pricing and quantity guards", () => {
  it("selects the tier at each pricing boundary", () => {
    expect(tierForQuantity(product.tiers, "1")?.unit_price_e4).toBe(95_000);
    expect(tierForQuantity(product.tiers, "100")?.unit_price_e4).toBe(88_000);
    expect(tierForQuantity(product.tiers, "500")?.unit_price_e4).toBe(82_000);
  });

  it("adds at MOQ, groups by merchant, and totals with fixed-point bigint", () => {
    const result = addProduct([], product);
    expect(result.error).toBeUndefined();
    expect(result.cart[0]?.quantity).toBe("1");
    expect(result.cart[0]?.merchantId).toBe("merchant-cn-demo");
    expect(lineTotalE4(result.cart[0]!)).toBe(95_000n);

    const hundred = updateQuantity(result.cart, product.public_id, "100");
    expect(lineTotalE4(hundred.cart[0]!)).toBe(8_800_000n);
    expect(cartTotalE4(hundred.cart)).toBe(8_800_000n);
    expect(cartCount(hundred.cart)).toBe("100");
  });

  it("rejects fractional quantities without changing the cart", () => {
    const result = addProduct([], product);
    const fractional = updateQuantity(result.cart, product.public_id, "1.5");
    expect(fractional.error).toBe("invalidQuantity");
    expect(fractional.cart[0]?.quantity).toBe("1");
  });

  it("matches integer tier boundaries and multiplies estimated money with bigint", () => {
    const result = addProduct([], product);
    const hundred = updateQuantity(result.cart, product.public_id, "100");
    expect(hundred.error).toBeUndefined();
    expect(tierForQuantity(product.tiers, hundred.cart[0]!.quantity)?.unit_price_e4).toBe(88_000);
    expect(lineTotalE4(hundred.cart[0]!)).toBe(8_800_000n);
  });

  it("removes at quantity zero and rejects below MOQ or over available", () => {
    const moqTwo = { ...product, tiers: [{ min_qty: "2", unit_price_e4: 95_000 }] };
    const added = addProduct([], moqTwo);
    const zero = updateQuantity(added.cart, product.public_id, "0");
    expect(zero.error).toBeUndefined();
    expect(zero.cart).toHaveLength(0);

    const below = updateQuantity(added.cart, product.public_id, "1");
    expect(below.error).toBe("belowMoq");
    expect(below.cart[0]?.quantity).toBe("2");

    const over = updateQuantity(added.cart, product.public_id, "101", "100");
    expect(over.error).toBe("overAvailable");
    expect(over.cart[0]?.quantity).toBe("2");
  });
});
