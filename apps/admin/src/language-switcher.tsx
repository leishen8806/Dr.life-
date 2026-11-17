import { Segmented } from "antd";
import { applyDocumentLanguage, storeLanguagePreference, useI18n, type Language } from "@drlife/i18n";

export function AdminLanguageSwitcher() {
  const { language, setLanguage, t } = useI18n();
  const options = [{ value: "zh", label: t("language.zh") }, { value: "en", label: t("language.en") }, { value: "km", label: t("language.km") }];
  return <Segmented aria-label={t("language.select")} value={language} options={options} onChange={(value) => { const next = value as Language; storeLanguagePreference(next); applyDocumentLanguage(next); setLanguage(next); }} />;
}
