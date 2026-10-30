import { Children, isValidElement, type ReactElement, type ReactNode } from "react";
import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
import type { Language } from "@drlife/i18n";
import { ErrorState } from "./foundation";
import {
  accountPhase,
  failurePhase,
  throttleWaiting,
  tickSeconds,
  type StartupAccount,
  type Translate
} from "./startup";
import {
  AccountBlockedView,
  BrandSplashView,
  LanguageChoiceView,
  LoginFailedView,
  LoginThrottledView
} from "./startup-screens";

type Props = { children?: ReactNode } & Record<string, unknown>;

/** Walks the tree, calling function components, so assertions see the final elements. */
function walk(node: ReactNode, visit: (element: ReactElement<Props>) => void): void {
  for (const child of Children.toArray(node)) {
    if (!isValidElement<Props>(child)) continue;
    visit(child);
    if (typeof child.type === "function") {
      walk((child.type as (props: Props) => ReactNode)(child.props), visit);
    } else {
      walk(child.props.children, visit);
    }
  }
}

function elements(node: ReactNode): ReactElement<Props>[] {
  const found: ReactElement<Props>[] = [];
  walk(node, (element) => found.push(element));
  return found;
}

function texts(node: ReactNode): string[] {
  return elements(node).flatMap((element) =>
    Children.toArray(element.props.children).filter(
      (child): child is string => typeof child === "string"
    )
  );
}

function buttons(node: ReactNode): ReactElement<Props>[] {
  return elements(node).filter((element) => element.type === "button");
}

/** Keys and parameters are visible in the output, so the assertions stay language-free. */
const translate: Translate = (key, params) =>
  [key, ...Object.values(params ?? {})].join(" ");

const account = (patch: Partial<StartupAccount> = {}): StartupAccount => ({
  lang: "en",
  status: "active",
  needsLanguage: false,
  ...patch
});

function declaration(selector: string, property: string): boolean {
  const styles = readFileSync(new URL("./styles.css", import.meta.url), "utf8");
  let start = styles.indexOf(selector);
  while (start >= 0) {
    const end = styles.indexOf("}", start);
    if (styles.slice(start, end).includes(property)) return true;
    start = styles.indexOf(selector, start + selector.length);
  }
  return false;
}

describe("startup phases", () => {
  it("routes a blocked account to its own page and keeps no retry window", () => {
    expect(failurePhase({ code: 2006, retryAfterSeconds: 60 })).toEqual({
      name: "blocked"
    });
    expect(accountPhase(account({ status: "frozen" }))).toEqual({ name: "blocked" });
    expect(accountPhase(account({ status: "disabled" }))).toEqual({ name: "blocked" });
  });

  it("keeps the 2007 lockout window and falls back when the server sent none", () => {
    expect(failurePhase({ code: 2007, retryAfterSeconds: 45 })).toEqual({
      name: "throttled",
      seconds: 45
    });
    expect(failurePhase({ code: 2007, retryAfterSeconds: 2.2 })).toEqual({
      name: "throttled",
      seconds: 3
    });
    expect(failurePhase({ code: 2007 })).toEqual({ name: "throttled" });
  });

  it("maps login failures onto trilingual copy instead of raw codes", () => {
    expect(failurePhase({ code: 2005 })).toEqual({
      name: "failed",
      messageKey: "error.2005"
    });
    expect(failurePhase({ code: 3001 })).toEqual({
      name: "failed",
      messageKey: "error.1001"
    });
    expect(failurePhase(new TypeError("fetch failed"))).toEqual({
      name: "failed",
      messageKey: "common.networkError"
    });
  });

  it("asks for a language once per account and then hands over to the app", () => {
    expect(accountPhase(account({ needsLanguage: true }))).toEqual({ name: "language" });
    expect(accountPhase(account({ lang: null }))).toEqual({ name: "language" });
    expect(accountPhase(account({ lang: "km" }))).toEqual({
      name: "ready",
      account: account({ lang: "km" })
    });
  });

  it("counts the lockout down to zero and no further", () => {
    expect(tickSeconds(2)).toBe(1);
    expect(tickSeconds(1)).toBe(0);
    expect(tickSeconds(0)).toBe(0);
    expect(tickSeconds(undefined)).toBeUndefined();
    expect([3, 1].map(throttleWaiting)).toEqual([true, true]);
    expect([0, undefined].map(throttleWaiting)).toEqual([false, false]);
  });
});

describe("startup screens", () => {
  it("offers the skip only while the brand halo is the reason for waiting", () => {
    const onSkip = vi.fn();
    const brand = BrandSplashView({
      translate,
      statusKey: "startup.brandLoading",
      onSkip
    });
    expect(texts(brand)).toContain("brand.slogan");
    expect(texts(brand)).toContain("startup.brandLoading");
    (buttons(brand)[0]!.props.onClick as () => void)();
    expect(onSkip).toHaveBeenCalledTimes(1);

    const signingIn = BrandSplashView({ translate, statusKey: "startup.signingIn" });
    expect(texts(signingIn)).toContain("startup.signingIn");
    expect(buttons(signingIn)).toHaveLength(0);
  });

  it("marks the Telegram language, registers the pick, and locks while saving", () => {
    const onSelect = vi.fn();
    const view = LanguageChoiceView({ translate, suggested: "km", onSelect });
    const options = buttons(view);

    expect(texts(view)).toContain("startup.languageHint");
    expect(
      options.map((option) => texts(option)[0])
    ).toEqual(["language.zh", "language.en", "language.km"]);
    expect(texts(options[0]!)).not.toContain("startup.telegramSuggested");
    expect(texts(options[2]!)).toContain("startup.telegramSuggested");
    (options[2]!.props.onClick as () => void)();
    expect(onSelect).toHaveBeenCalledWith("km" satisfies Language);

    const busy = LanguageChoiceView({ translate, busy: true, failed: true, onSelect });
    expect(buttons(busy).every((option) => option.props.disabled === true)).toBe(true);
    expect(texts(busy)).toContain("startup.languageFailed");
  });

  it("shows 2006 as an account status with no retry entry point", () => {
    const view = AccountBlockedView({ translate });

    expect(view.type).toBe(ErrorState);
    expect(view.props).toEqual({
      title: "startup.blockedTitle",
      description: "error.2006"
    });
    expect(texts(view)).toContain("error.2006");
    expect(buttons(view)).toHaveLength(0);
  });

  it("counts 2007 down and only then offers sign-in again", () => {
    const onRetry = vi.fn();
    const waiting = LoginThrottledView({ translate, seconds: 45, onRetry });
    expect(texts(waiting)).toContain("error.2007.countdown 45");
    expect(buttons(waiting)).toHaveLength(0);

    const elapsed = LoginThrottledView({ translate, seconds: 0, onRetry });
    expect(texts(elapsed)).toContain("error.2007");
    (buttons(elapsed)[0]!.props.onClick as () => void)();
    expect(onRetry).toHaveBeenCalledTimes(1);

    const noWindow = LoginThrottledView({ translate, onRetry });
    expect(texts(noWindow)).toContain("error.2007");
    expect(buttons(noWindow)).toHaveLength(1);
  });

  it("lets the user ask for another attempt after 2005 instead of looping", () => {
    const onRetry = vi.fn();
    const view = LoginFailedView({ translate, messageKey: "error.2005", onRetry });

    expect(texts(view)).toContain("error.2005");
    expect(texts(view)).toContain("startup.signInAgain");
    (buttons(view)[0]!.props.onClick as () => void)();
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("lets Khmer wrap instead of clipping it", () => {
    expect(declaration(".drl-splash__slogan", "overflow-wrap: anywhere")).toBe(true);
    expect(declaration(".drl-language-choice__options button", "white-space: normal")).toBe(true);
    expect(declaration(".drl-language-choice__options button", "overflow: visible")).toBe(true);
    expect(declaration(".drl-language-choice__options button", "overflow-wrap: anywhere")).toBe(true);
  });
});
