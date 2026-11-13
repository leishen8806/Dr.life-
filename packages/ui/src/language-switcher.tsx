import {
  applyDocumentLanguage,
  storeLanguagePreference,
  type Language,
  type TranslationKey,
  useI18n
} from "@drlife/i18n";
import { Button } from "./foundation";

/** Display order of the three supported languages; shared with the first-run chooser. */
export const languages: Language[] = ["zh", "en", "km"];
export const languageKeys: Record<Language, TranslationKey> = {
  zh: "language.zh",
  en: "language.en",
  km: "language.km"
};

interface LanguageSwitcherViewProps {
  language: Language;
  translate: (key: TranslationKey) => string;
  onSelect: (language: Language) => void;
}

export function LanguageSwitcherView({
  language,
  translate,
  onSelect
}: LanguageSwitcherViewProps) {
  return (
    <div
      className="drl-language-switcher"
      aria-label={translate("language.select")}
    >
      {languages.map((item) => (
        <Button
          key={item}
          variant="secondary"
          onClick={() => onSelect(item)}
          ariaPressed={language === item}
        >
          {translate(languageKeys[item])}
        </Button>
      ))}
    </div>
  );
}

export function LanguageSwitcher({
  onChange
}: {
  onChange?: (language: Language) => void;
}) {
  const { language, setLanguage, t } = useI18n();
  const select = (nextLanguage: Language) => {
    storeLanguagePreference(nextLanguage);
    applyDocumentLanguage(nextLanguage);
    setLanguage(nextLanguage);
    onChange?.(nextLanguage);
  };

  return (
    <LanguageSwitcherView language={language} translate={t} onSelect={select} />
  );
}
