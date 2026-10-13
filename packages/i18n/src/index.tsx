import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState
} from "react";
import en from "./locales/en.json";
import km from "./locales/km.json";
import zh from "./locales/zh.json";

export type Language = "zh" | "en" | "km";
export type TranslationKey = keyof typeof en;
type Params = Record<string, string | number>;

export interface LanguageStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

const languageStorageKey = "drlife_lang";

const locales: Record<Language, Record<TranslationKey, string>> = {
  zh,
  en,
  km
};

function supportedLanguage(value?: string | null): Language | undefined {
  const language = value?.toLowerCase().split(/[-_]/)[0];
  return language === "zh" || language === "en" || language === "km"
    ? language
    : undefined;
}

export function readLanguagePreference(
  storage?: LanguageStorage
): Language | undefined {
  try {
    const target = storage ?? globalThis.localStorage;
    return supportedLanguage(target.getItem(languageStorageKey));
  } catch {
    return undefined;
  }
}

export function storeLanguagePreference(
  language: Language,
  storage?: LanguageStorage
): void {
  try {
    const target = storage ?? globalThis.localStorage;
    target.setItem(languageStorageKey, language);
  } catch {
    // Storage is optional in restricted browsers and server-side rendering.
  }
}

export function applyDocumentLanguage(
  language: Language,
  root: Pick<HTMLElement, "lang"> | undefined = globalThis.document
    ?.documentElement
): void {
  if (root) root.lang = language;
}

export function resolveLanguage(
  accountLanguage?: string | null,
  telegramLanguage?: string | null
): Language {
  return (
    supportedLanguage(accountLanguage) ??
    supportedLanguage(telegramLanguage) ??
    "en"
  );
}

export function t(
  key: TranslationKey,
  params: Params = {},
  language: Language = "en"
): string {
  return Object.entries(params).reduce(
    (text, [name, value]) => text.replaceAll(`{${name}}`, String(value)),
    locales[language][key]
  );
}

export function plural<T>(count: number, one: T, other: T): T {
  return count === 1 ? one : other;
}

interface I18nContextValue {
  language: Language;
  setLanguage: (language: Language) => void;
  t: (key: TranslationKey, params?: Params) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({
  initialLanguage,
  children
}: {
  initialLanguage: Language;
  children: ReactNode;
}) {
  const [language, setLanguage] = useState(initialLanguage);
  useEffect(() => {
    applyDocumentLanguage(language);
  }, [language]);
  const translate = useCallback(
    (key: TranslationKey, params: Params = {}) => t(key, params, language),
    [language]
  );
  const value = useMemo(
    () => ({ language, setLanguage, t: translate }),
    [language, translate]
  );
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const context = useContext(I18nContext);
  if (!context) throw new Error("useI18n must be used inside I18nProvider");
  return context;
}
