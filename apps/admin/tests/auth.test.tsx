import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@drlife/i18n";
import { AuthGate, LoginPage, PasswordChangePage } from "../src/auth";

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

describe("admin authentication acceptance matrix", () => {
  afterEach(() => { window.history.replaceState({}, "", "/login"); });

  it("shows one uniform error for a failed login", async () => {
    mockApi.login.mockRejectedValue(new Error("invalid credentials"));
    render(<I18nProvider initialLanguage="en"><LoginPage onSuccess={vi.fn()} /></I18nProvider>);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "admin@example.test" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "wrong" } });
    fireEvent.change(screen.getByLabelText("Verification code"), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect((await screen.findByRole("alert")).textContent).toContain("account, password or verification code is incorrect");
  });

  it("renders the Retry-After message for rate limiting", async () => {
    mockApi.login.mockRejectedValue(Object.assign(new Error("rate limited"), { status: 429, retryAfter: "30" }));
    render(<I18nProvider initialLanguage="en"><LoginPage onSuccess={vi.fn()} /></I18nProvider>);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "admin@example.test" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password" } });
    fireEvent.change(screen.getByLabelText("Verification code"), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect((await screen.findByRole("alert")).textContent).toContain("wait 30 seconds");
  });

  it("requires matching passwords and submits the step-up TOTP for a password change", async () => {
    const onComplete = vi.fn();
    mockApi.changePassword.mockResolvedValue(undefined);
    render(<I18nProvider initialLanguage="en"><PasswordChangePage onComplete={onComplete} /></I18nProvider>);
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "old-password" } });
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "new-password-012345678901" } });
    fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: "different" } });
    fireEvent.change(screen.getByLabelText("Verification code"), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Save password" }));
    expect(await screen.findByRole("alert")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: "new-password-012345678901" } });
    fireEvent.click(screen.getByRole("button", { name: "Save password" }));
    await waitFor(() => expect(mockApi.changePassword).toHaveBeenCalledWith({ currentPassword: "old-password", newPassword: "new-password-012345678901", confirmPassword: "new-password-012345678901", totpCode: "123456" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("returns to login when refresh fails", async () => {
    window.history.replaceState({}, "", "/administrators");
    mockApi.refresh.mockRejectedValue(new Error("expired"));
    render(<I18nProvider initialLanguage="en"><AuthGate><div>private admin area</div></AuthGate></I18nProvider>);
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeTruthy();
    expect(mockApi.refresh).toHaveBeenCalledOnce();
  });
});
