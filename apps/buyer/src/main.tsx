import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createTelegramSession } from "@drlife/api-client/session";
import {
  I18nProvider,
  readLanguagePreference,
  resolveLanguage,
  supportedLanguage
} from "@drlife/i18n";
import {
  getTelegramEnvironment,
  applyMiniAppTheme,
  mountTelegramTheme,
  StartupGate
} from "@drlife/ui";
import "@drlife/ui/styles.css";
import "./styles.css";
import { BuyerApp } from "./app";

const environment = getTelegramEnvironment();
mountTelegramTheme();
applyMiniAppTheme(environment.colorScheme);

const initialLanguage = resolveLanguage(
  readLanguagePreference(),
  environment.languageCode
);

// Created once: a new session identity would start the sign-in over.
// initData is read per call so a refresh replays the current launch parameters.
const session = createTelegramSession({
  end: "buyer",
  initData: () => getTelegramEnvironment().initData,
  getLanguage: () => readLanguagePreference() ?? initialLanguage
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider initialLanguage={initialLanguage}>
      <StartupGate
        gateway={session}
        suggested={supportedLanguage(environment.languageCode)}
      >
        <BuyerApp isTelegram={environment.isTelegram} client={session.client} />
      </StartupGate>
    </I18nProvider>
  </StrictMode>
);
