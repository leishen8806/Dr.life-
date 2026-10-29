import { describe, expect, it } from "vitest";
import {
  checkProductDraft,
  draftFromProduct,
  emptyProductDraft,
  formatPriceE4,
  normalizeDecimal,
  parsePriceE4,
  toUpdateRequest,
  unpublishPatch,
  type Product,
  type ProductDraft
} from "./form";

function validDraft(): ProductDraft {
  return {
    categoryCode: "rice",
    name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
    spec: { zh: "", en: "", km: "" },
    unit: "bag",
    origin: "cn",
    leadDays: "7",
    volM3: "0.0125",
    tiers: [
      { minQty: "10", unitPrice: "12.5" },
      { minQty: "100", unitPrice: "11.8" }
    ]
  };
}

function fieldsOf(draft: ProductDraft): string[] {
  return checkProductDraft(draft).errors.map((error) => error.field);
}

describe("normalizeDecimal", () => {
  it("normalises the way the backend catalog.ParseDecimal does", () => {
    expect(normalizeDecimal("1.500", 3)).toBe("1.5");
    expect(normalizeDecimal("0012", 3)).toBe("12");
    expect(normalizeDecimal("0.0", 3)).toBe("0");
    expect(normalizeDecimal(" 1.5 ", 3)).toBe("1.5");
    expect(normalizeDecimal("1", 3)).toBe("1");
  });

  it("rejects everything the backend rejects", () => {
    for (const value of ["", "   ", "1.2345", "-1", "1e3", "1.", ".5", "1.2.3", "abc", "1,5"]) {
      expect(normalizeDecimal(value, 3)).toBeUndefined();
    }
  });
});

describe("price conversion", () => {
  it("parses US dollars into ten-thousandths without floating point", () => {
    expect(parsePriceE4("1.2345")).toBe(12345);
    expect(parsePriceE4("1")).toBe(10000);
    expect(parsePriceE4("0")).toBe(0);
    expect(parsePriceE4("0.005")).toBe(50);
    expect(parsePriceE4("11.80")).toBe(118000);
  });

  it("rejects prices the backend column cannot hold", () => {
    for (const value of ["", "1.23456", "-1", "abc"]) {
      expect(parsePriceE4(value)).toBeUndefined();
    }
  });

  it("formats ten-thousandths back into an editable dollar string", () => {
    expect(formatPriceE4(12345)).toBe("1.2345");
    expect(formatPriceE4(10000)).toBe("1");
    expect(formatPriceE4(0)).toBe("0");
    expect(formatPriceE4(50)).toBe("0.005");
  });
});

describe("checkProductDraft", () => {
  it("accepts a complete draft and maps it onto the create request", () => {
    const { errors, request } = checkProductDraft(validDraft());

    expect(errors).toEqual([]);
    expect(request).toEqual({
      category_code: "rice",
      name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
      unit: "bag",
      origin: "cn",
      lead_days: 7,
      vol_m3: "0.0125",
      source: "manual",
      tiers: [
        { min_qty: "10", unit_price_e4: 125000 },
        { min_qty: "100", unit_price_e4: 118000 }
      ]
    });
  });

  it("requires the name in all three languages", () => {
    const draft = validDraft();
    draft.name.km = "   ";

    const { errors, request } = checkProductDraft(draft);

    expect(errors).toEqual([
      { field: "name.km", messageKey: "product.error.nameTrilingual" }
    ]);
    expect(request).toBeUndefined();
  });

  it("reports every missing language of the name at once", () => {
    const draft = validDraft();
    draft.name = { zh: "", en: "", km: "" };

    expect(fieldsOf(draft)).toEqual(["name.zh", "name.en", "name.km"]);
  });

  it("treats the spec as all three languages or none", () => {
    const partial = validDraft();
    partial.spec = { zh: "25kg/包", en: "", km: "" };

    expect(fieldsOf(partial)).toEqual(["spec.en", "spec.km"]);

    const complete = validDraft();
    complete.spec = { zh: "25kg/包", en: "25kg/bag", km: "25kg/ថង់" };

    expect(checkProductDraft(complete).request?.spec).toEqual({
      zh: "25kg/包",
      en: "25kg/bag",
      km: "25kg/ថង់"
    });
  });

  it("requires the category and the unit", () => {
    const draft = validDraft();
    draft.categoryCode = "";
    draft.unit = " ";

    expect(checkProductDraft(draft).errors).toEqual([
      { field: "category_code", messageKey: "product.error.categoryRequired" },
      { field: "unit", messageKey: "product.error.unitRequired" }
    ]);
  });

  it("accepts an empty lead time and volume, and rejects malformed ones", () => {
    const empty = validDraft();
    empty.leadDays = "";
    empty.volM3 = "";

    const { errors, request } = checkProductDraft(empty);
    expect(errors).toEqual([]);
    expect(request?.lead_days).toBeNull();
    expect(request?.vol_m3).toBeNull();

    for (const leadDays of ["-1", "1.5", "七"]) {
      const draft = validDraft();
      draft.leadDays = leadDays;
      expect(fieldsOf(draft)).toEqual(["lead_days"]);
    }

    for (const volume of ["-1", "0.00001", "123456789"]) {
      const draft = validDraft();
      draft.volM3 = volume;
      expect(fieldsOf(draft)).toEqual(["vol_m3"]);
    }
  });

  it("requires at least two price tiers", () => {
    const draft = validDraft();
    draft.tiers = [{ minQty: "10", unitPrice: "12.5" }];

    expect(checkProductDraft(draft).errors).toEqual([
      { field: "tiers", messageKey: "product.error.tiersTooFew" }
    ]);
  });

  it("requires distinct minimum quantities after normalisation", () => {
    const draft = validDraft();
    draft.tiers = [
      { minQty: "1.5", unitPrice: "12.5" },
      { minQty: "1.500", unitPrice: "11.8" }
    ];

    expect(checkProductDraft(draft).errors).toEqual([
      { field: "tiers[1].min_qty", messageKey: "product.error.minQtyDuplicate" }
    ]);
  });

  it("requires a positive minimum quantity and a non-negative unit price", () => {
    const draft = validDraft();
    draft.tiers = [
      { minQty: "0", unitPrice: "12.5" },
      { minQty: "10", unitPrice: "-1" }
    ];

    expect(checkProductDraft(draft).errors).toEqual([
      { field: "tiers[0].min_qty", messageKey: "product.error.minQtyInvalid" },
      {
        field: "tiers[1].unit_price_e4",
        messageKey: "product.error.unitPriceInvalid"
      }
    ]);
  });

  it("rejects a minimum quantity with more than three decimals", () => {
    const draft = validDraft();
    draft.tiers[0] = { minQty: "1.2345", unitPrice: "12.5" };

    expect(fieldsOf(draft)).toEqual(["tiers[0].min_qty"]);
  });
});

describe("request builders", () => {
  it("turns a create request into a patch without the immutable source", () => {
    const request = checkProductDraft(validDraft()).request;

    expect(request).toBeDefined();
    expect(toUpdateRequest(request!)).toEqual({
      category_code: "rice",
      name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
      spec: undefined,
      unit: "bag",
      origin: "cn",
      lead_days: 7,
      vol_m3: "0.0125",
      tiers: [
        { min_qty: "10", unit_price_e4: 125000 },
        { min_qty: "100", unit_price_e4: 118000 }
      ]
    });
  });

  it("takes a product off sale with the only status the merchant may set", () => {
    expect(unpublishPatch()).toEqual({ status: "off" });
  });
});

describe("draftFromProduct", () => {
  const product: Product = {
    public_id: "01HZY0000000000000000MERCH",
    category_code: "rice",
    name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
    unit: "bag",
    origin: "lc",
    lead_days: null,
    vol_m3: null,
    status: "draft",
    source: "manual",
    tiers: [
      { min_qty: "10", unit_price_e4: 125000 },
      { min_qty: "100", unit_price_e4: 50 }
    ],
    updated_at: "2026-10-29T00:00:00Z"
  };

  it("fills the edit form from the server product", () => {
    expect(draftFromProduct(product)).toEqual({
      categoryCode: "rice",
      name: { zh: "泰国香米 5kg", en: "Jasmine rice 5kg", km: "អង្ករម្លិះ 5kg" },
      spec: { zh: "", en: "", km: "" },
      unit: "bag",
      origin: "lc",
      leadDays: "",
      volM3: "",
      tiers: [
        { minQty: "10", unitPrice: "12.5" },
        { minQty: "100", unitPrice: "0.005" }
      ]
    } satisfies ProductDraft);
  });

  it("round-trips a server product back into the same create request", () => {
    const { errors, request } = checkProductDraft(draftFromProduct(product));

    expect(errors).toEqual([]);
    expect(request?.tiers).toEqual(product.tiers);
  });

  it("starts a new product on two blank tiers so the minimum is visible", () => {
    const draft = emptyProductDraft();

    expect(draft.tiers).toHaveLength(2);
    expect(fieldsOf(draft)).toContain("name.zh");
  });
});
