import {
  backButton,
  init,
  mainButton,
  miniApp,
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
  colorScheme: "light" | "dark";
}

export function resolveMiniAppColorScheme(isDark: boolean): "light" | "dark" {
  return isDark ? "dark" : "light";
}

function isDarkThemeColor(value: string | undefined): boolean {
  if (!value || !/^#[0-9a-f]{6}$/i.test(value)) return false;
  const red = Number.parseInt(value.slice(1, 3), 16);
  const green = Number.parseInt(value.slice(3, 5), 16);
  const blue = Number.parseInt(value.slice(5, 7), 16);
  return (red * 299 + green * 587 + blue * 114) / 1000 < 128;
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
      backgroundColor: launch.tgWebAppThemeParams.bgColor ?? "#ffffff",
      colorScheme: isDarkThemeColor(launch.tgWebAppThemeParams.bgColor)
        ? "dark"
        : resolveMiniAppColorScheme(miniApp.isDark())
    };
  } catch {
    let backgroundColor = "#f4f6f8";
    let colorScheme: "light" | "dark" = "light";
    if (typeof window !== "undefined") {
      try {
        const raw = new URLSearchParams(window.location.search).get("tgWebAppThemeParams");
        const parsed = raw ? JSON.parse(raw) as { bg_color?: string } : undefined;
        backgroundColor = parsed?.bg_color ?? backgroundColor;
        colorScheme = isDarkThemeColor(backgroundColor) ? "dark" : "light";
      } catch {
        // Browser-only launch parameter parsing is best-effort in local evidence runs.
      }
    }
    return {
      isTelegram: false,
      initData: "dev_mock_init_data",
      languageCode: "en",
      backgroundColor,
      colorScheme
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
  if (!initialized) {
    try {
      init();
      initialized = true;
    } catch {
      return;
    }
  }
  if (!initialized || !themeParams.mountSync.isAvailable()) return;
  themeParams.mountSync();
  if (themeParams.bindCssVars.isAvailable()) themeParams.bindCssVars();
}

export function watchTelegramTheme(onChange: (colorScheme: "light" | "dark") => void) {
  if (!initialized || typeof miniApp.isDark !== "function") return () => undefined;
  const signal = miniApp.isDark as unknown as {
    sub?: (listener: (current: boolean) => void) => () => void;
  };
  if (typeof signal.sub !== "function") return () => undefined;
  return signal.sub((current) => onChange(resolveMiniAppColorScheme(current)));
}

export function applyMiniAppTheme(colorScheme: "light" | "dark") {
  document.documentElement.dataset.surface = "miniapp";
  document.documentElement.dataset.colorScheme = colorScheme;
}
