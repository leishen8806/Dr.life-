import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@drlife/i18n";
import { LoginPage } from "../src/auth";

const mockApi = vi.hoisted(() => ({ login: vi.fn(), refresh: vi.fn(), logout: vi.fn(), changePassword: vi.fn() }));
vi.mock("../src/api", () => ({ api: mockApi }));

afterEach(() => { cleanup(); vi.clearAllMocks(); });

it("submits email, password and TOTP without writing tokens to storage", async () => {
  mockApi.login.mockResolvedValue({});
  render(<I18nProvider initialLanguage="en"><LoginPage onSuccess={vi.fn()} /></I18nProvider>);
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: "admin@example.test" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password" } });
  fireEvent.change(screen.getByLabelText("Verification code"), { target: { value: "123456" } });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  expect(await screen.findByRole("button", { name: "Sign in" })).toBeTruthy();
  expect(mockApi.login).toHaveBeenCalledWith({ email: "admin@example.test", password: "password", totpCode: "123456" });
  expect(localStorage.length).toBe(0); expect(sessionStorage.length).toBe(0);
});
