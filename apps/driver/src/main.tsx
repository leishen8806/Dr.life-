import { StrictMode, useCallback, useMemo } from "react";
import { createRoot } from "react-dom/client";
import {
  I18nProvider,
  readLanguagePreference,
  resolveLanguage,
  useI18n
} from "@drlife/i18n";

const languageKeys = {
  zh: "language.zh",
  en: "language.en",
  km: "language.km"
} as const;
import {
  getTelegramEnvironment,
  LanguageSwitcher,
  mountTelegramTheme,
  useBackButton,
  useMainButton
} from "@drlife/ui";
import "@drlife/ui/styles.css";
import "./styles.css";

const environment = getTelegramEnvironment();
mountTelegramTheme();

function App() {
  const { language, t } = useI18n();
  const noOp = useCallback(() => undefined, []);
  useBackButton(noOp, environment.isTelegram);
  useMainButton(noOp, t("common.confirm"), environment.isTelegram);
  const languageName = useMemo(() => t(languageKeys[language]), [language, t]);

  return (
    <main
      className="mini-shell"
      style={{ backgroundColor: environment.backgroundColor }}
    >
      <header>
        <p className="eyebrow">Dr.life+</p>
        <h1>{t("app.driver")}</h1>
        <strong
          className={environment.isTelegram ? "mode telegram" : "mode dev"}
        >
          {t(
            environment.isTelegram
              ? "shell.telegramMode"
              : "shell.developmentMode"
          )}
        </strong>
      </header>
      <section className="card">
        <p>{t("shell.currentLanguage", { language: languageName })}</p>
        <p>{t("shell.initData", { length: environment.initData.length })}</p>
        <p>{t("shell.theme", { color: environment.backgroundColor })}</p>
        <LanguageSwitcher />
      </section>
      <section className="card typography-demo">
        <p lang="zh">{t("demo.zh")}</p>
        <p lang="en">{t("demo.en")}</p>
        <p lang="km" className="drl-khmer">
          {t("demo.km")}
        </p>
      </section>
      <footer>
        <span>{t("shell.backButtonDemo")}</span>
        <span>{t("shell.mainButtonDemo")}</span>
      </footer>
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider
      initialLanguage={resolveLanguage(
        readLanguagePreference(),
        environment.languageCode
      )}
    >
      <App />
    </I18nProvider>
  </StrictMode>
);
