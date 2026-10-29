import type { components } from "@drlife/api-client";
import type { TranslationKey } from "@drlife/i18n";

export type Product = components["schemas"]["Product"];
export type ProductCreateRequest = components["schemas"]["ProductCreateRequest"];
export type ProductUpdateRequest = components["schemas"]["ProductUpdateRequest"];
type LocalizedText = components["schemas"]["LocalizedText"];
type Tier = components["schemas"]["ProductTier"];

/** 三语顺序与 backend/internal/catalog/validate.go 的 languages 一致，报错顺序才对得上。 */
const languages = ["zh", "en", "km"] as const;
type LanguageKey = (typeof languages)[number];

const qtyScale = 3; // catalog.product_tier.min_qty numeric(18,3)
const priceScale = 4; // unit_price_e4 以万分之一美元计，全程不经浮点
const volumeScale = 4; // catalog.product.vol_m3 numeric(12,4)
const volumeIntegerDigits = 8;

export type LocalizedDraft = Record<LanguageKey, string>;

export interface TierDraft {
  minQty: string;
  unitPrice: string;
}

/** 表单里的值一律是字符串，用户没填完也能原样留着，提交时才转成接口类型。 */
export interface ProductDraft {
  categoryCode: string;
  name: LocalizedDraft;
  spec: LocalizedDraft;
  unit: string;
  origin: Product["origin"];
  leadDays: string;
  volM3: string;
  tiers: TierDraft[];
}

export interface ProductFieldError {
  /** 与后端 ValidationError.Field 同形，便于对照日志定位。 */
  field: string;
  messageKey: TranslationKey;
}

export interface ProductDraftCheck {
  errors: ProductFieldError[];
  /** 只在 errors 为空时给出，没过校验的草稿就没法送上接口。 */
  request?: ProductCreateRequest;
}

function isDigits(value: string): boolean {
  return value.length > 0 && !/\D/.test(value);
}

function blankLocalized(): LocalizedDraft {
  return { zh: "", en: "", km: "" };
}

function trimLocalized(text: LocalizedDraft): LocalizedText {
  return { zh: text.zh.trim(), en: text.en.trim(), km: text.km.trim() };
}

function filledLanguages(text: LocalizedDraft): number {
  return languages.filter((language) => text[language].trim().length > 0).length;
}

/** catalog.ParseDecimal 的前端对照实现：拒绝负号、科学计数法与超出标度的小数。 */
export function normalizeDecimal(raw: string, scale: number): string | undefined {
  const value = raw.trim();
  const separator = value.indexOf(".");
  const integerPart = separator === -1 ? value : value.slice(0, separator);
  const fractionPart = separator === -1 ? "" : value.slice(separator + 1);
  if (!isDigits(integerPart)) return undefined;
  if (separator !== -1 && (!isDigits(fractionPart) || fractionPart.length > scale)) {
    return undefined;
  }
  const integer = integerPart.replace(/^0+/, "") || "0";
  const fraction = fractionPart.replace(/0+$/, "");
  return fraction ? `${integer}.${fraction}` : integer;
}

export function parsePriceE4(raw: string): number | undefined {
  const normalized = normalizeDecimal(raw, priceScale);
  if (normalized === undefined) return undefined;
  const [integer = "0", fraction = ""] = normalized.split(".");
  const value = Number(integer + fraction.padEnd(priceScale, "0"));
  return Number.isSafeInteger(value) ? value : undefined;
}

export function formatPriceE4(value: number): string {
  const digits = String(Math.abs(Math.trunc(value))).padStart(priceScale + 1, "0");
  const integer = digits.slice(0, digits.length - priceScale);
  const fraction = digits.slice(digits.length - priceScale).replace(/0+$/, "");
  const magnitude = fraction ? `${integer}.${fraction}` : integer;
  return value < 0 ? `-${magnitude}` : magnitude;
}

/** 对应 catalog.product 的 CHECK (lead_days IS NULL OR lead_days >= 0)。 */
export function parseLeadDays(raw: string): number | undefined {
  const value = raw.trim();
  if (!isDigits(value)) return undefined;
  const days = Number(value);
  return Number.isSafeInteger(days) ? days : undefined;
}

export function parseVolume(raw: string): string | undefined {
  const normalized = normalizeDecimal(raw, volumeScale);
  if (normalized === undefined) return undefined;
  const [integer = ""] = normalized.split(".");
  return integer.length > volumeIntegerDigits ? undefined : normalized;
}

export function emptyProductDraft(): ProductDraft {
  return {
    categoryCode: "",
    name: blankLocalized(),
    spec: blankLocalized(),
    unit: "",
    origin: "cn",
    leadDays: "",
    volM3: "",
    // 先摆出两档，商家一眼看到上架要求的最少档位，不用等报错才知道。
    tiers: [
      { minQty: "", unitPrice: "" },
      { minQty: "", unitPrice: "" }
    ]
  };
}

export function draftFromProduct(product: Product): ProductDraft {
  return {
    categoryCode: product.category_code,
    name: { ...product.name },
    spec: product.spec ? { ...product.spec } : blankLocalized(),
    unit: product.unit,
    origin: product.origin,
    leadDays: product.lead_days == null ? "" : String(product.lead_days),
    volM3: product.vol_m3 ?? "",
    tiers: product.tiers.map((tier) => ({
      minQty: tier.min_qty,
      unitPrice: formatPriceE4(tier.unit_price_e4)
    }))
  };
}

function checkTiers(draft: ProductDraft, errors: ProductFieldError[]): Tier[] {
  if (draft.tiers.length < 2) {
    errors.push({ field: "tiers", messageKey: "product.error.tiersTooFew" });
    return [];
  }
  const tiers: Tier[] = [];
  const seen = new Set<string>();
  draft.tiers.forEach((tier, index) => {
    const minQty = normalizeDecimal(tier.minQty, qtyScale);
    const unitPriceE4 = parsePriceE4(tier.unitPrice);
    if (minQty === undefined || minQty === "0") {
      errors.push({
        field: `tiers[${index}].min_qty`,
        messageKey: "product.error.minQtyInvalid"
      });
    } else if (seen.has(minQty)) {
      errors.push({
        field: `tiers[${index}].min_qty`,
        messageKey: "product.error.minQtyDuplicate"
      });
    } else {
      seen.add(minQty);
    }
    if (unitPriceE4 === undefined) {
      errors.push({
        field: `tiers[${index}].unit_price_e4`,
        messageKey: "product.error.unitPriceInvalid"
      });
    }
    if (minQty !== undefined && unitPriceE4 !== undefined) {
      tiers.push({ min_qty: minQty, unit_price_e4: unitPriceE4 });
    }
  });
  return tiers;
}

export function checkProductDraft(draft: ProductDraft): ProductDraftCheck {
  const errors: ProductFieldError[] = [];
  if (!draft.categoryCode.trim()) {
    errors.push({
      field: "category_code",
      messageKey: "product.error.categoryRequired"
    });
  }
  for (const language of languages) {
    if (!draft.name[language].trim()) {
      errors.push({
        field: `name.${language}`,
        messageKey: "product.error.nameTrilingual"
      });
    }
  }
  // 规格是可选的 LocalizedText，接口没法只收一两种语言，所以要么三语齐全要么全空。
  const specLanguages = filledLanguages(draft.spec);
  if (specLanguages > 0 && specLanguages < languages.length) {
    for (const language of languages) {
      if (!draft.spec[language].trim()) {
        errors.push({
          field: `spec.${language}`,
          messageKey: "product.error.specTrilingual"
        });
      }
    }
  }
  if (!draft.unit.trim()) {
    errors.push({ field: "unit", messageKey: "product.error.unitRequired" });
  }
  const tiers = checkTiers(draft, errors);
  const leadDays = draft.leadDays.trim() ? parseLeadDays(draft.leadDays) : null;
  if (leadDays === undefined) {
    errors.push({ field: "lead_days", messageKey: "product.error.leadDaysInvalid" });
  }
  const volM3 = draft.volM3.trim() ? parseVolume(draft.volM3) : null;
  if (volM3 === undefined) {
    errors.push({ field: "vol_m3", messageKey: "product.error.volumeInvalid" });
  }

  // leadDays / volM3 的 undefined 判断同时充当类型收窄，errors 非空时已经包含它们。
  if (errors.length > 0 || leadDays === undefined || volM3 === undefined) {
    return { errors };
  }
  return {
    errors,
    request: {
      category_code: draft.categoryCode.trim(),
      name: trimLocalized(draft.name),
      spec:
        specLanguages === languages.length ? trimLocalized(draft.spec) : undefined,
      unit: draft.unit.trim(),
      origin: draft.origin,
      lead_days: leadDays,
      vol_m3: volM3,
      source: "manual",
      tiers
    }
  };
}

/** source 建完就不再改，PATCH 不带它；status 另有专用入口。 */
export function toUpdateRequest(request: ProductCreateRequest): ProductUpdateRequest {
  return {
    category_code: request.category_code,
    name: request.name,
    spec: request.spec,
    unit: request.unit,
    origin: request.origin,
    lead_days: request.lead_days,
    vol_m3: request.vol_m3,
    tiers: request.tiers
  };
}

/** 没有独立的下架接口：商家只能把状态移到 draft 或 off，on 只能经 publish 到达。 */
export function unpublishPatch(): ProductUpdateRequest {
  return { status: "off" };
}
