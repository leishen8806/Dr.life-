import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createApiClient } from "@drlife/api-client";
import {
  I18nProvider,
  readLanguagePreference,
  resolveLanguage
} from "@drlife/i18n";
import { getTelegramEnvironment, mountTelegramTheme } from "@drlife/ui";
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

// 商家端登录属于另一片，本卡不含鉴权：没有令牌时接口回 2001，界面照常给出提示。
// LanguageSwitcher 会把选择写进偏好存储，所以这里读偏好就是当前语言。
const api = createMerchantProductApi(
  createApiClient({
    getLanguage: () => readLanguagePreference() ?? initialLanguage
  })
);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider initialLanguage={initialLanguage}>
      <ProductScreen api={api} />
    </I18nProvider>
  </StrictMode>
);
