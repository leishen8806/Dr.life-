import {
  applyDocumentLanguage,
  storeLanguagePreference,
  type Language,
  type TranslationKey,
  useI18n
} from "@drlife/i18n";

const languages: Language[] = ["zh", "en", "km"];
const languageKeys: Record<Language, TranslationKey> = {
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
        <button
          key={item}
          type="button"
          onClick={() => onSelect(item)}
          aria-pressed={language === item}
        >
          {translate(languageKeys[item])}
        </button>
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
