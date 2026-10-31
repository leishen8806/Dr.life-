import type { Language, TranslationKey } from "@drlife/i18n";

/** Same signature as `useI18n().t`, so a screen can be rendered without the provider in tests. */
export type Translate = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string;

/** The account fields the startup flow reads; `@drlife/api-client`'s SessionAccount satisfies it. */
export interface StartupAccount {
  lang: Language | null;
  status: "active" | "frozen" | "disabled";
  needsLanguage: boolean;
}

/** Implemented per end, because the login endpoint takes the end and the role owns the /me path. */
export interface StartupGateway {
  login(): Promise<StartupAccount>;
  registerLanguage(language: Language): Promise<StartupAccount>;
}

export type StartupPhase =
  /** Brand load: small mark plus halo, about 1.5s, skippable. */
  | { name: "brand" }
  | { name: "signingIn" }
  /** First language registration; one language per account. */
  | { name: "language" }
  /** Agreement consent (A-09 / CMP-634) lands here in the next slice. */
  | { name: "consent" }
  | { name: "ready"; account: StartupAccount }
  /** 2006, and a frozen or disabled account on an otherwise successful login. */
  | { name: "blocked" }
  /** 2007; `seconds` is the server's lockout window, undefined when it gave none. */
  | { name: "throttled"; seconds?: number }
  | { name: "failed"; messageKey: TranslationKey };

/** Login failures that have their own copy; anything else falls back below. */
const loginMessageKeys: Record<number, TranslationKey> = {
  1001: "error.1001",
  1002: "error.1002",
  1004: "error.1004",
  1005: "error.1005",
  2001: "error.2001",
  2002: "error.2002",
  2005: "error.2005"
};

interface ErrorLike {
  code?: unknown;
  retryAfterSeconds?: unknown;
}

/**
 * Reads `ApiError`'s shape structurally so this package keeps no dependency on the
 * API client. 2006 is a status rather than a transient failure, so it gets its own
 * page with no retry entry; 2007 keeps the window the server sent.
 */
export function failurePhase(error: unknown): StartupPhase {
  const failure = (error ?? {}) as ErrorLike;
  const code = typeof failure.code === "number" ? failure.code : undefined;
  if (code === 2006) return { name: "blocked" };
  if (code === 2007) {
    const seconds = failure.retryAfterSeconds;
    return typeof seconds === "number" && Number.isFinite(seconds)
      ? { name: "throttled", seconds: Math.max(0, Math.ceil(seconds)) }
      : { name: "throttled" };
  }
  if (code === undefined) return { name: "failed", messageKey: "common.networkError" };
  return { name: "failed", messageKey: loginMessageKeys[code] ?? "error.1001" };
}

/** A frozen or disabled account never reaches the app, even when the login call succeeded. */
export function accountPhase(account: StartupAccount): StartupPhase {
  if (account.status !== "active") return { name: "blocked" };
  if (account.needsLanguage || !account.lang) return { name: "language" };
  return { name: "ready", account };
}

/** One 2007 tick; at 0 the sign-in button opens again. */
export function tickSeconds(seconds: number | undefined): number | undefined {
  return seconds === undefined ? undefined : Math.max(0, seconds - 1);
}

/** Without a window from the server there is nothing to count down, so retry stays open. */
export function throttleWaiting(seconds: number | undefined): boolean {
  return seconds !== undefined && seconds > 0;
}
