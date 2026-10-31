import type { Language } from "@drlife/i18n";
import { ErrorState } from "./foundation";
import { languageKeys, languages } from "./language-switcher";
import { throttleWaiting, type Translate } from "./startup";

/**
 * Brand load: small mark plus halo, about 1.5s. The skip button is only offered while
 * the halo is the reason we are waiting — once sign-in is the reason there is nothing
 * to skip to.
 */
export function BrandSplashView({
  translate,
  statusKey,
  onSkip
}: {
  translate: Translate;
  statusKey: "startup.brandLoading" | "startup.signingIn";
  onSkip?: () => void;
}) {
  return (
    <section className="drl-splash" role="status" aria-live="polite">
      <span className="drl-splash__halo" aria-hidden="true">
        <span className="drl-splash__mark">+</span>
      </span>
      <strong className="drl-splash__brand">Dr.life+</strong>
      <p className="drl-splash__slogan">{translate("brand.slogan")}</p>
      <p className="drl-splash__status">{translate(statusKey)}</p>
      {onSkip && (
        <button type="button" className="drl-splash__skip" onClick={onSkip}>
          {translate("startup.skip")}
        </button>
      )}
    </section>
  );
}

/**
 * First-run language registration. The Telegram language is marked as suggested, and
 * picking a button registers it for this account — one language per account.
 */
export function LanguageChoiceView({
  translate,
  suggested,
  busy = false,
  failed = false,
  onSelect
}: {
  translate: Translate;
  suggested?: Language;
  busy?: boolean;
  failed?: boolean;
  onSelect: (language: Language) => void;
}) {
  return (
    <section
      className="drl-language-choice"
      role="dialog"
      aria-modal="true"
      aria-label={translate("language.select")}
    >
      <h1>{translate("language.select")}</h1>
      <p>{translate("startup.languageHint")}</p>
      {failed && (
        <p className="drl-language-choice__failure" role="alert">
          {translate("startup.languageFailed")}
        </p>
      )}
      <div className="drl-language-choice__options">
        {languages.map((item) => (
          <button
            key={item}
            type="button"
            disabled={busy}
            onClick={() => onSelect(item)}
          >
            <span>{translate(languageKeys[item])}</span>
            {item === suggested && (
              <small>{translate("startup.telegramSuggested")}</small>
            )}
          </button>
        ))}
      </div>
    </section>
  );
}

/**
 * 2006. A frozen or disabled account is a status, not a transient failure, so this page
 * carries no retry entry point and the copy never suggests one.
 */
export function AccountBlockedView({ translate }: { translate: Translate }) {
  return (
    <ErrorState
      title={translate("startup.blockedTitle")}
      description={translate("error.2006")}
    />
  );
}

/**
 * 2007. The countdown comes from `Retry-After`; sign-in reopens when it reaches 0, and
 * a response without a window falls back to the plain wait message.
 */
export function LoginThrottledView({
  translate,
  seconds,
  onRetry
}: {
  translate: Translate;
  seconds?: number;
  onRetry: () => void;
}) {
  const waiting = throttleWaiting(seconds);
  return (
    <ErrorState
      title={translate("startup.throttledTitle")}
      description={
        waiting
          ? translate("error.2007.countdown", { seconds: seconds ?? 0 })
          : translate("error.2007")
      }
      retryLabel={waiting ? undefined : translate("startup.signInAgain")}
      onRetry={waiting ? undefined : onRetry}
    />
  );
}

/** 2005 and the rest: the user asks for another attempt, the app never re-logs in by itself. */
export function LoginFailedView({
  translate,
  messageKey,
  onRetry
}: {
  translate: Translate;
  messageKey: Parameters<Translate>[0];
  onRetry: () => void;
}) {
  return (
    <ErrorState
      title={translate("startup.loginFailedTitle")}
      description={translate(messageKey)}
      retryLabel={translate("startup.signInAgain")}
      onRetry={onRetry}
    />
  );
}
