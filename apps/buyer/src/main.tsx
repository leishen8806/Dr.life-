import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import {
  I18nProvider,
  readLanguagePreference,
  resolveLanguage
} from "@drlife/i18n";
import { getTelegramEnvironment, mountTelegramTheme } from "@drlife/ui";
import "@drlife/ui/styles.css";
import "./styles.css";
import { BuyerApp } from "./app";

const environment = getTelegramEnvironment();
mountTelegramTheme();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider
      initialLanguage={resolveLanguage(
        readLanguagePreference(),
        environment.languageCode
      )}
    >
      <BuyerApp isTelegram={environment.isTelegram} />
    </I18nProvider>
  </StrictMode>
);
