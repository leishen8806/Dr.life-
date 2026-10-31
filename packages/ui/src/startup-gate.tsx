import { type ReactNode, useCallback, useEffect, useState } from "react";
import {
  applyDocumentLanguage,
  storeLanguagePreference,
  type Language,
  useI18n
} from "@drlife/i18n";
import {
  accountPhase,
  failurePhase,
  throttleWaiting,
  tickSeconds,
  type StartupGateway,
  type StartupPhase
} from "./startup";
import {
  AccountBlockedView,
  BrandSplashView,
  LanguageChoiceView,
  LoginFailedView,
  LoginThrottledView
} from "./startup-screens";

/** About 1.5s of brand load, per the interaction document. */
const brandMillis = 1_500;

export interface Startup {
  phase: StartupPhase;
  /** A language registration is in flight. */
  busy: boolean;
  languageFailed: boolean;
  skipBrand: () => void;
  retry: () => void;
  selectLanguage: (language: Language) => void;
}

/**
 * Brand load, automatic sign-in, and first language registration.
 *
 * `gateway` must be stable (create it once outside the component): every new identity
 * starts a fresh sign-in.
 */
export function useStartup(gateway: StartupGateway): Startup {
  const [brandHeld, setBrandHeld] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const [outcome, setOutcome] = useState<StartupPhase>();
  const [busy, setBusy] = useState(false);
  const [languageFailed, setLanguageFailed] = useState(false);

  useEffect(() => {
    const timer = setTimeout(() => setBrandHeld(false), brandMillis);
    return () => clearTimeout(timer);
  }, []);

  // Sign-in runs behind the halo. A failed login never re-runs on its own: 2005 means the
  // initData was rejected, and retrying it in a loop would just burn the 2007 budget.
  useEffect(() => {
    let live = true;
    gateway.login().then(
      (account) => {
        if (live) setOutcome(accountPhase(account));
      },
      (error: unknown) => {
        if (live) setOutcome(failurePhase(error));
      }
    );
    return () => {
      live = false;
    };
  }, [gateway, attempt]);

  // 2007 counts the server's window down once per second and then opens sign-in again.
  useEffect(() => {
    if (outcome?.name !== "throttled" || !throttleWaiting(outcome.seconds)) return;
    const timer = setTimeout(
      () => setOutcome({ name: "throttled", seconds: tickSeconds(outcome.seconds) }),
      1_000
    );
    return () => clearTimeout(timer);
  }, [outcome]);

  const selectLanguage = useCallback(
    (language: Language) => {
      setBusy(true);
      setLanguageFailed(false);
      gateway.registerLanguage(language).then(
        (account) => {
          setBusy(false);
          setOutcome(accountPhase(account));
        },
        (error: unknown) => {
          setBusy(false);
          const next = failurePhase(error);
          // A blocked account takes over the screen; anything else keeps the chooser so
          // the user can pick again instead of landing on a dead end.
          if (next.name === "blocked") setOutcome(next);
          else setLanguageFailed(true);
        }
      );
    },
    [gateway]
  );

  const skipBrand = useCallback(() => setBrandHeld(false), []);
  // Clearing the outcome here rather than in the effect keeps the effect free of
  // synchronous state writes, and the previous failure leaves the screen at once.
  const retry = useCallback(() => {
    setOutcome(undefined);
    setAttempt((value) => value + 1);
  }, []);
  const phase: StartupPhase = brandHeld
    ? { name: "brand" }
    : (outcome ?? { name: "signingIn" });

  return { phase, busy, languageFailed, skipBrand, retry, selectLanguage };
}

/**
 * Renders the startup screens and hands over to the app once the account is signed in,
 * active and has a registered language.
 */
export function StartupGate({
  gateway,
  suggested,
  children
}: {
  gateway: StartupGateway;
  /** Language suggested by Telegram; marked as such in the first-run chooser. */
  suggested?: Language;
  children: ReactNode;
}) {
  const { setLanguage, t } = useI18n();
  const startup = useStartup(gateway);
  const { phase } = startup;

  // One account, one language: what the server registered wins over the local preference.
  useEffect(() => {
    if (phase.name !== "ready" || !phase.account.lang) return;
    const language = phase.account.lang;
    storeLanguagePreference(language);
    applyDocumentLanguage(language);
    setLanguage(language);
  }, [phase, setLanguage]);

  const select = startup.selectLanguage;
  const choose = useCallback(
    (language: Language) => {
      // Switch straight away so the confirmation or the failure reads in the new language.
      setLanguage(language);
      select(language);
    },
    [select, setLanguage]
  );

  switch (phase.name) {
    case "brand":
      return (
        <BrandSplashView
          translate={t}
          statusKey="startup.brandLoading"
          onSkip={startup.skipBrand}
        />
      );
    case "signingIn":
      return <BrandSplashView translate={t} statusKey="startup.signingIn" />;
    case "language":
      return (
        <LanguageChoiceView
          translate={t}
          suggested={suggested}
          busy={startup.busy}
          failed={startup.languageFailed}
          onSelect={choose}
        />
      );
    case "blocked":
      return <AccountBlockedView translate={t} />;
    case "throttled":
      return (
        <LoginThrottledView
          translate={t}
          seconds={phase.seconds}
          onRetry={startup.retry}
        />
      );
    case "failed":
      return (
        <LoginFailedView
          translate={t}
          messageKey={phase.messageKey}
          onRetry={startup.retry}
        />
      );
    case "consent":
      // A-09 (CMP-634) renders the agreement window here. Until it merges the flow never
      // enters this phase, and nothing in this package invents a consent endpoint.
      return null;
    case "ready":
      return <>{children}</>;
  }
}
