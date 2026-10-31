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
  mountTelegramTheme,
  StartupGate
} from "@drlife/ui";
import "@drlife/ui/styles.css";
import "./styles.css";
import { createMerchantProductApi } from "./products/api";
import { ProductScreen } from "./products/product-screen";

const environment = getTelegramEnvironment();
mountTelegramTheme();

const initialLanguage = resolveLanguage(
  readLanguagePreference(),
  environment.languageCode
);

// Created once: a new session identity would start the sign-in over.
// initData is read per call so a refresh replays the current launch parameters.
const session = createTelegramSession({
  end: "merchant",
  initData: () => getTelegramEnvironment().initData,
  getLanguage: () => readLanguagePreference() ?? initialLanguage
});

// The product screen runs behind the gate, so its client is the signed-in one.
const api = createMerchantProductApi(session.client);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider initialLanguage={initialLanguage}>
      <StartupGate
        gateway={session}
        suggested={supportedLanguage(environment.languageCode)}
      >
        <ProductScreen api={api} />
      </StartupGate>
    </I18nProvider>
  </StrictMode>
);
