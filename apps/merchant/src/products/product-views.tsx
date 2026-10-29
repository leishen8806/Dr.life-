import type { ReactNode } from "react";
import type { Language, TranslationKey } from "@drlife/i18n";
import { Card, EmptyState, ListItem, StatusTag } from "@drlife/ui";
import {
  formatPriceE4,
  type Category,
  type LocalizedDraft,
  type Product,
  type ProductDraft,
  type ProductFieldError,
  type TierDraft
} from "./form";
import { productActions, statusLabelKey, statusTone } from "./status";

export type Translate = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string;

const languageOrder: Language[] = ["zh", "en", "km"];
const languageNameKeys: Record<Language, TranslationKey> = {
  zh: "language.zh",
  en: "language.en",
  km: "language.km"
};
const originOrder: Product["origin"][] = ["cn", "lc"];
const originKeys: Record<Product["origin"], TranslationKey> = {
  cn: "product.origin.cn",
  lc: "product.origin.lc"
};

/** 起步档是起订量最小的那一档，不一定是接口返回的第一条。 */
function entryTier(product: Product): Product["tiers"][number] | undefined {
  return product.tiers.reduce<Product["tiers"][number] | undefined>(
    (lowest, tier) =>
      lowest && Number(lowest.min_qty) <= Number(tier.min_qty) ? lowest : tier,
    undefined
  );
}

function withLanguage(
  text: LocalizedDraft,
  language: Language,
  value: string
): LocalizedDraft {
  return {
    zh: language === "zh" ? value : text.zh,
    en: language === "en" ? value : text.en,
    km: language === "km" ? value : text.km
  };
}

export function ProductStatusTag({
  status,
  translate
}: {
  status: Product["status"];
  translate: Translate;
}) {
  return (
    <StatusTag tone={statusTone(status)}>
      {translate(statusLabelKey(status))}
    </StatusTag>
  );
}

export function ProductListView({
  products,
  language,
  translate,
  onCreate,
  onEdit,
  onPublish,
  onUnpublish
}: {
  products: Product[];
  language: Language;
  translate: Translate;
  onCreate: () => void;
  onEdit: (product: Product) => void;
  onPublish: (product: Product) => void;
  onUnpublish: (product: Product) => void;
}) {
  if (products.length === 0) {
    return (
      <EmptyState
        title={translate("product.list.empty")}
        actionLabel={translate("product.list.create")}
        onAction={onCreate}
      />
    );
  }
  return (
    <Card>
      <ul className="merchant-products">
        {products.map((product) => {
          const actions = productActions(product.status);
          const entry = entryTier(product);
          return (
            <li key={product.public_id} className="merchant-products__item">
              <ListItem
                title={product.name[language]}
                description={
                  entry &&
                  translate("product.tier.summary", {
                    price: formatPriceE4(entry.unit_price_e4),
                    quantity: entry.min_qty,
                    unit: product.unit
                  })
                }
                trailing={
                  <ProductStatusTag
                    status={product.status}
                    translate={translate}
                  />
                }
              />
              <div className="merchant-products__actions">
                {actions.canEdit && (
                  <button type="button" onClick={() => onEdit(product)}>
                    {translate("product.action.edit")}
                  </button>
                )}
                {actions.canPublish && (
                  <button type="button" onClick={() => onPublish(product)}>
                    {translate("product.action.publish")}
                  </button>
                )}
                {actions.canUnpublish && (
                  <button
                    type="button"
                    className="drl-button--danger"
                    onClick={() => onUnpublish(product)}
                  >
                    {translate("product.action.unpublish")}
                  </button>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

function Field({
  label,
  error,
  children
}: {
  label: string;
  error?: string | undefined;
  children: ReactNode;
}) {
  return (
    <label className="merchant-field">
      <span className="merchant-field__label">{label}</span>
      {children}
      {error && (
        <small className="merchant-field__error" role="alert">
          {error}
        </small>
      )}
    </label>
  );
}

export function ProductFormView({
  mode,
  draft,
  errors,
  categories,
  language,
  translate,
  busy,
  onChange,
  onSubmit,
  onCancel
}: {
  mode: "create" | "edit";
  draft: ProductDraft;
  errors: ProductFieldError[];
  categories: Category[];
  language: Language;
  translate: Translate;
  busy: boolean;
  onChange: (draft: ProductDraft) => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  const message = (field: string): string | undefined => {
    const found = errors.find((error) => error.field === field);
    return found && translate(found.messageKey);
  };
  const patch = (change: Partial<ProductDraft>) =>
    onChange({ ...draft, ...change });
  const patchTier = (index: number, change: Partial<TierDraft>) =>
    patch({
      tiers: draft.tiers.map((tier, position) =>
        position === index ? { ...tier, ...change } : tier
      )
    });

  return (
    <Card>
      <form
        className="merchant-form"
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <h2>
          {translate(
            mode === "create"
              ? "product.form.createTitle"
              : "product.form.editTitle"
          )}
        </h2>

        <Field
          label={translate("product.field.category")}
          error={message("category_code")}
        >
          <select
            value={draft.categoryCode}
            onChange={(event) => patch({ categoryCode: event.target.value })}
          >
            <option value="" />
            {categories.map((category) => (
              <option key={category.code} value={category.code}>
                {category.name[language]}
              </option>
            ))}
          </select>
        </Field>

        {languageOrder.map((item) => (
          <Field
            key={`name-${item}`}
            label={translate("product.field.name", {
              language: translate(languageNameKeys[item])
            })}
            error={message(`name.${item}`)}
          >
            <input
              lang={item}
              className={item === "km" ? "drl-khmer" : undefined}
              value={draft.name[item]}
              onChange={(event) =>
                patch({ name: withLanguage(draft.name, item, event.target.value) })
              }
            />
          </Field>
        ))}

        {languageOrder.map((item) => (
          <Field
            key={`spec-${item}`}
            label={translate("product.field.spec", {
              language: translate(languageNameKeys[item])
            })}
            error={message(`spec.${item}`)}
          >
            <input
              lang={item}
              className={item === "km" ? "drl-khmer" : undefined}
              value={draft.spec[item]}
              onChange={(event) =>
                patch({ spec: withLanguage(draft.spec, item, event.target.value) })
              }
            />
          </Field>
        ))}

        <Field label={translate("product.field.unit")} error={message("unit")}>
          <input
            value={draft.unit}
            onChange={(event) => patch({ unit: event.target.value })}
          />
        </Field>

        <Field label={translate("product.field.origin")}>
          <select
            value={draft.origin}
            onChange={(event) =>
              patch({ origin: event.target.value as Product["origin"] })
            }
          >
            {originOrder.map((origin) => (
              <option key={origin} value={origin}>
                {translate(originKeys[origin])}
              </option>
            ))}
          </select>
        </Field>

        <Field
          label={translate("product.field.leadDays")}
          error={message("lead_days")}
        >
          <input
            inputMode="numeric"
            value={draft.leadDays}
            onChange={(event) => patch({ leadDays: event.target.value })}
          />
        </Field>

        <Field
          label={translate("product.field.volume")}
          error={message("vol_m3")}
        >
          <input
            inputMode="decimal"
            value={draft.volM3}
            onChange={(event) => patch({ volM3: event.target.value })}
          />
        </Field>

        <fieldset className="merchant-form__tiers">
          <legend>{translate("product.field.tiers")}</legend>
          {message("tiers") && (
            <small className="merchant-field__error" role="alert">
              {message("tiers")}
            </small>
          )}
          {draft.tiers.map((tier, index) => (
            <div key={`tier-${index}`} className="merchant-form__tier">
              <Field
                label={translate("product.field.minQty")}
                error={message(`tiers[${index}].min_qty`)}
              >
                <input
                  inputMode="decimal"
                  value={tier.minQty}
                  onChange={(event) =>
                    patchTier(index, { minQty: event.target.value })
                  }
                />
              </Field>
              <Field
                label={translate("product.field.unitPrice")}
                error={message(`tiers[${index}].unit_price_e4`)}
              >
                <input
                  inputMode="decimal"
                  value={tier.unitPrice}
                  onChange={(event) =>
                    patchTier(index, { unitPrice: event.target.value })
                  }
                />
              </Field>
              {/* 上架至少要两档，所以只有多出来的档位才给删除入口。 */}
              {draft.tiers.length > 2 && (
                <button
                  type="button"
                  onClick={() =>
                    patch({
                      tiers: draft.tiers.filter(
                        (_tier, position) => position !== index
                      )
                    })
                  }
                >
                  {translate("product.tier.remove", { index: index + 1 })}
                </button>
              )}
            </div>
          ))}
          <button
            type="button"
            onClick={() =>
              patch({ tiers: [...draft.tiers, { minQty: "", unitPrice: "" }] })
            }
          >
            {translate("product.tier.add")}
          </button>
        </fieldset>

        <footer className="merchant-form__actions">
          <button type="button" onClick={onCancel}>
            {translate("common.cancel")}
          </button>
          <button type="submit" disabled={busy}>
            {translate("common.save")}
          </button>
        </footer>
      </form>
    </Card>
  );
}
