import {
  backButton,
  init,
  mainButton,
  retrieveLaunchParams,
  retrieveRawInitData,
  themeParams
} from "@telegram-apps/sdk";
import { useEffect } from "react";

export interface TelegramEnvironment {
  isTelegram: boolean;
  initData: string;
  languageCode?: string;
  backgroundColor: string;
}

let initialized = false;

export function getTelegramEnvironment(): TelegramEnvironment {
  try {
    const launch = retrieveLaunchParams(true);
    if (!initialized) {
      init();
      initialized = true;
    }
    return {
      isTelegram: true,
      initData: retrieveRawInitData() ?? "",
      languageCode: launch.tgWebAppData?.user?.languageCode,
      backgroundColor: launch.tgWebAppThemeParams.bgColor ?? "#ffffff"
    };
  } catch {
    return {
      isTelegram: false,
      initData: "dev_mock_init_data",
      languageCode: "en",
      backgroundColor: "#f4f6f8"
    };
  }
}

export function useBackButton(onClick: () => void, enabled = true) {
  useEffect(() => {
    if (!enabled || !backButton.mount.isAvailable()) return;
    backButton.mount();
    backButton.show();
    const off = backButton.onClick(onClick);
    return () => {
      off();
      if (backButton.hide.isAvailable()) backButton.hide();
      backButton.unmount();
    };
  }, [enabled, onClick]);
}

export function useMainButton(
  onClick: () => void,
  text: string,
  enabled = true
) {
  useEffect(() => {
    if (!enabled || !mainButton.mount.isAvailable()) return;
    mainButton.mount();
    mainButton.setParams({ text, isEnabled: true, isVisible: true });
    const off = mainButton.onClick(onClick);
    return () => {
      off();
      mainButton.unmount();
    };
  }, [enabled, onClick, text]);
}

export function mountTelegramTheme() {
  if (!initialized || !themeParams.mountSync.isAvailable()) return;
  themeParams.mountSync();
  if (themeParams.bindCssVars.isAvailable()) themeParams.bindCssVars();
}
