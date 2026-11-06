import { useCallback, useEffect, useState } from "react";
import type { Language } from "@drlife/i18n";
import { ErrorState, Loading } from "./foundation";
import { languageKeys, languages } from "./language-switcher";
import {
  throttleWaiting,
  type ConsentKind,
  type LegalDocument,
  type StartupAccount,
  type StartupGateway,
  type Translate
} from "./startup";

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

const consentKinds: ConsentKind[] = ["privacy", "terms"];

/** A-09. Business screens never render while a current document still needs consent. */
export function ConsentView({
  translate,
  language,
  gateway,
  onComplete
}: {
  translate: Translate;
  language: Language;
  gateway: StartupGateway;
  onComplete: (account: StartupAccount) => void;
}) {
  const [documents, setDocuments] = useState<LegalDocument[]>([]);
  const [required, setRequired] = useState<ConsentKind[]>([]);
  const [checked, setChecked] = useState<Partial<Record<ConsentKind, boolean>>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<"load" | "accept">();
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    Promise.all([gateway.getConsentState(), gateway.getLegalDocuments()]).then(
      ([statuses, currentDocuments]) => {
        if (!live) return;
        setRequired(statuses.filter((status) => status.needsReaccept).map((status) => status.kind));
        setChecked(
          Object.fromEntries(
            statuses.filter((status) => status.accepted && !status.needsReaccept).map((status) => [status.kind, true])
          )
        );
        setDocuments(currentDocuments);
        setLoading(false);
      },
      () => {
        if (!live) return;
        setLoading(false);
        setFailed("load");
      }
    );
    return () => {
      live = false;
    };
  }, [gateway, attempt]);

  const load = useCallback(() => {
    setLoading(true);
    setFailed(undefined);
    setAttempt((value) => value + 1);
  }, []);

  if (loading) return <Loading label={translate("common.loading")} />;
  if (failed === "load")
    return (
      <ErrorState
        title={translate("consent.title")}
        description={translate("consent.loadFailed")}
        retryLabel={translate("common.retry")}
        onRetry={load}
      />
    );

  const canContinue = required.every((kind) => checked[kind] === true);
  const accept = async () => {
    if (!canContinue || busy) return;
    setBusy(true);
    setFailed(undefined);
    try {
      let account: StartupAccount | undefined;
      for (const kind of required) {
        const document = documents.find((item) => item.kind === kind);
        if (!document) throw new Error("The current legal document is unavailable");
        account = await gateway.acceptConsent(kind, document.version);
      }
      if (account) onComplete(account);
    } catch {
      setBusy(false);
      setFailed("accept");
    }
  };

  return (
    <section className="drl-consent" role="dialog" aria-modal="true" aria-labelledby="drl-consent-title">
      <h1 id="drl-consent-title">{translate("consent.title")}</h1>
      <p>{translate("consent.description")}</p>
      {failed === "accept" && <p className="drl-consent__failure" role="alert">{translate("consent.acceptFailed")}</p>}
      <div className="drl-consent__documents">
        {consentKinds.map((kind) => {
          const document = documents.find((item) => item.kind === kind);
          const isRequired = required.includes(kind);
          if (!document) return null;
          return (
            <article className="drl-consent__document" key={kind}>
              <label>
                <input
                  type="checkbox"
                  checked={checked[kind] === true}
                  disabled={!isRequired || busy}
                  onChange={(event) => setChecked((current) => ({ ...current, [kind]: event.target.checked }))}
                />
                <span>
                  <strong>{translate(`consent.${kind}`)}</strong>
                  <small>{translate("consent.version", { version: document.version })}</small>
                </span>
                {isRequired && <em>{translate("consent.required")}</em>}
              </label>
              <details>
                <summary>{translate("consent.read")}</summary>
                <p>{document.body[language]}</p>
              </details>
            </article>
          );
        })}
      </div>
      <button type="button" disabled={!canContinue || busy} onClick={accept}>
        {translate("consent.accept")}
      </button>
    </section>
  );
}
