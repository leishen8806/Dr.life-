import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ isDark: Object.assign(vi.fn(), { sub: vi.fn() }) }));

vi.mock("@telegram-apps/sdk", () => ({
  backButton: { mount: { isAvailable: () => false } },
  init: vi.fn(),
  mainButton: { mount: { isAvailable: () => false } },
  miniApp: { isDark: mocks.isDark },
  retrieveLaunchParams: vi.fn(() => ({
    tgWebAppData: { user: { languageCode: "en" } },
    tgWebAppThemeParams: { bgColor: "#ffffff" }
  })),
  retrieveRawInitData: vi.fn(() => "init-data"),
  themeParams: { mountSync: { isAvailable: () => false } }
}));

import { getTelegramEnvironment, resolveMiniAppColorScheme, watchTelegramTheme } from "./telegram";

describe("Telegram theme mapping", () => {
  it("returns a no-op cleanup when the Telegram theme subscription is unavailable", () => {
    const listener = vi.fn();
    mocks.isDark.sub.mockReturnValueOnce(undefined);
    const cleanup = watchTelegramTheme(listener);
    expect(cleanup()).toBeUndefined();
    expect(listener).not.toHaveBeenCalled();
    mocks.isDark.sub.mockReset();
  });

  it("maps the SDK miniApp dark signal to the document theme contract", () => {
    expect(resolveMiniAppColorScheme(true)).toBe("dark");
    expect(resolveMiniAppColorScheme(false)).toBe("light");

    mocks.isDark.mockReturnValueOnce(true);
    expect(getTelegramEnvironment().colorScheme).toBe("dark");

    mocks.isDark.mockReturnValueOnce(false);
    expect(getTelegramEnvironment().colorScheme).toBe("light");
  });

  it("subscribes to Telegram dark-mode changes after initialization", () => {
    const listener = vi.fn();
    const stop = vi.fn();
    mocks.isDark.sub.mockImplementationOnce((callback: (value: boolean) => void) => {
      callback(true);
      return stop;
    });
    expect(watchTelegramTheme(listener)).toBe(stop);
    expect(listener).toHaveBeenCalledWith("dark");
  });
});
