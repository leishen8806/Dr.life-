import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ isDark: vi.fn() }));

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

import { getTelegramEnvironment, resolveMiniAppColorScheme } from "./telegram";

describe("Telegram theme mapping", () => {
  it("maps the SDK miniApp dark signal to the document theme contract", () => {
    expect(resolveMiniAppColorScheme(true)).toBe("dark");
    expect(resolveMiniAppColorScheme(false)).toBe("light");

    mocks.isDark.mockReturnValueOnce(true);
    expect(getTelegramEnvironment().colorScheme).toBe("dark");

    mocks.isDark.mockReturnValueOnce(false);
    expect(getTelegramEnvironment().colorScheme).toBe("light");
  });
});
