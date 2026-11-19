import { useState } from "react";
import { Button, Card, FormField, ListItem } from "@drlife/ui";
import type { TranslationKey } from "@drlife/i18n";

type Translate = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string;
const sourceKeys = ["1688", "amazon", "shopee", "csv"] as const;

export function MerchantV7ImportPage({
  t,
  onBack
}: {
  t: Translate;
  onBack: () => void;
}) {
  const [source, setSource] = useState<(typeof sourceKeys)[number]>("1688");
  const [rights, setRights] = useState(false);
  const [imported, setImported] = useState(false);
  return (
    <main className="mini-shell merchant-v7">
      <Button testId="merchant-back" variant="text" onClick={onBack}>
        {t("common.back")}
      </Button>
      <Card>
        <h1>{t("merchant.importProducts")}</h1>
        <FormField label={t("merchant.importSource")}>
          <select
            value={source}
            onChange={(event) => setSource(event.target.value as typeof source)}
          >
            {sourceKeys.map((key) => (
              <option key={key} value={key}>
                {t(`merchant.source.${key}` as TranslationKey)}
              </option>
            ))}
          </select>
        </FormField>
        <div className="merchant-v7__preview">
          <strong>{t("merchant.preview")}</strong>
          <ListItem
            title={t("merchant.sampleProduct")}
            description={t("merchant.waitingReview")}
          />
        </div>
        <label className="merchant-v7__rights">
          <input
            type="checkbox"
            checked={rights}
            onChange={(event) => setRights(event.target.checked)}
          />
          {t("merchant.rightsConfirm")}
        </label>
        {imported ? <p role="status">{t("merchant.imported")}</p> : null}
        <Button
          testId="merchant-import"
          disabled={!rights || !import.meta.env.DEV}
          onClick={() => setImported(true)}
        >
          {t("merchant.import")}
        </Button>
        {!import.meta.env.DEV && <p role="status">{t("common.comingSoon")}</p>}
      </Card>
    </main>
  );
}
