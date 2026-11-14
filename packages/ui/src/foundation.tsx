import React, { type KeyboardEvent, type ReactNode } from "react";
import type { Language, TranslationKey } from "@drlife/i18n";

export function AppHeader({
  brand = "Dr.life+",
  slogan,
  actions
}: {
  brand?: string;
  slogan: string;
  actions?: ReactNode;
}) {
  return (
    <header className="drl-app-header">
      <div>
        <strong className="drl-app-header__brand">{brand}</strong>
        <p className="drl-app-header__slogan">{slogan}</p>
      </div>
      {actions && <div className="drl-app-header__actions">{actions}</div>}
    </header>
  );
}

export interface NavigationItem {
  id: string;
  label: string;
  icon: ReactNode;
  badge?: ReactNode;
}

export function BottomNavigation({
  ariaLabel,
  items,
  activeId,
  onChange
}: {
  ariaLabel: string;
  items: NavigationItem[];
  activeId: string;
  onChange: (id: string) => void;
}) {
  return (
    <nav className="drl-bottom-navigation" aria-label={ariaLabel}>
      {items.map((item) => {
        const active = item.id === activeId;
        return (
          <button
            key={item.id}
            type="button"
            className="drl-bottom-navigation__item"
            aria-current={active ? "page" : undefined}
            onClick={() => onChange(item.id)}
          >
            <span className="drl-bottom-navigation__icon" aria-hidden="true">
              {item.icon}
            </span>
            <span className="drl-bottom-navigation__label">{item.label}</span>
            {item.badge !== undefined && (
              <span className="drl-bottom-navigation__badge">{item.badge}</span>
            )}
          </button>
        );
      })}
    </nav>
  );
}

export function Card({ children }: { children: ReactNode }) {
  return <section className="drl-card">{children}</section>;
}

export function ListItem({
  title,
  description,
  trailing,
  onClick
}: {
  title: string;
  description?: string;
  trailing?: ReactNode;
  onClick?: () => void;
}) {
  const content = (
    <>
      <span className="drl-list-item__content">
        <strong>{title}</strong>
        {description && <small>{description}</small>}
      </span>
      {trailing && <span className="drl-list-item__trailing">{trailing}</span>}
    </>
  );
  return onClick ? (
    <button type="button" className="drl-list-item" onClick={onClick}>
      {content}
    </button>
  ) : (
    <div className="drl-list-item">{content}</div>
  );
}

export function Stepper({
  steps,
  current
}: {
  steps: string[];
  current: number;
}) {
  return (
    <ol className="drl-stepper">
      {steps.map((step, index) => (
        <li
          key={step}
          className={index <= current ? "is-complete" : undefined}
          aria-current={index === current ? "step" : undefined}
        >
          {step}
        </li>
      ))}
    </ol>
  );
}

export function StatusTag({
  children,
  tone = "neutral"
}: {
  children: ReactNode;
  tone?: "neutral" | "success" | "warning" | "danger";
}) {
  return <span className={`drl-status-tag drl-status-tag--${tone}`}>{children}</span>;
}

function closeOnEscape(event: KeyboardEvent<HTMLElement>, onClose: () => void) {
  if (event.key === "Escape") onClose();
}

export function BottomSheet({
  open,
  title,
  closeLabel,
  onClose,
  children
}: {
  open: boolean;
  title: string;
  closeLabel: string;
  onClose: () => void;
  children: ReactNode;
}) {
  if (!open) return null;
  return (
    <div className="drl-overlay" onClick={onClose}>
      <section
        className="drl-bottom-sheet"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onKeyDown={(event) => closeOnEscape(event, onClose)}
        onClick={(event) => event.stopPropagation()}
      >
        <header>
          <h2>{title}</h2>
          <button type="button" aria-label={closeLabel} autoFocus onClick={onClose}>
            ×
          </button>
        </header>
        <div className="drl-bottom-sheet__content">{children}</div>
      </section>
    </div>
  );
}

export function Loading({
  label,
  skeleton = false
}: {
  label: string;
  skeleton?: boolean;
}) {
  return (
    <div className={`drl-loading${skeleton ? " drl-loading--skeleton" : ""}`} role="status" aria-live="polite">
      <span className="drl-loading__mark" aria-hidden="true">+</span>
      <span>{label}</span>
    </div>
  );
}

export function EmptyState({
  title,
  actionLabel,
  onAction
}: {
  title: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <section className="drl-state" role="status">
      <p>{title}</p>
      {actionLabel && onAction && <Button variant="secondary" onClick={onAction}>{actionLabel}</Button>}
    </section>
  );
}

export function ErrorState({
  title,
  description,
  retryLabel,
  onRetry
}: {
  title: string;
  description: string;
  retryLabel?: string;
  onRetry?: () => void;
}) {
  return (
    <section className="drl-state drl-state--error" role="alert">
      <strong>{title}</strong>
      <p>{description}</p>
      {retryLabel && onRetry && <button type="button" onClick={onRetry}>{retryLabel}</button>}
    </section>
  );
}

export function ConfirmDialog({
  open,
  title,
  description,
  cancelLabel,
  confirmLabel,
  onCancel,
  onConfirm
}: {
  open: boolean;
  title: string;
  description: string;
  cancelLabel: string;
  confirmLabel: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  if (!open) return null;
  return (
    <div className="drl-overlay" onClick={onCancel}>
      <section
        className="drl-confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onKeyDown={(event) => closeOnEscape(event, onCancel)}
        onClick={(event) => event.stopPropagation()}
      >
        <h2>{title}</h2>
        <p>{description}</p>
        <footer>
          <button type="button" autoFocus onClick={onCancel}>{cancelLabel}</button>
          <button type="button" className="drl-button--danger" onClick={onConfirm}>{confirmLabel}</button>
        </footer>
      </section>
    </div>
  );
}

export type ButtonVariant = "primary" | "secondary" | "danger" | "text";

export function Button({ children, variant = "primary", loading = false, disabled = false, type = "button", onClick, className, ariaPressed, ariaLabel, lang }: {
  children: ReactNode; variant?: ButtonVariant; loading?: boolean; disabled?: boolean;
  type?: "button" | "submit" | "reset"; onClick?: () => void; className?: string; ariaPressed?: boolean; ariaLabel?: string; lang?: string;
}) {
  return <button type={type} className={["drl-button", `drl-button--${variant}`, className].filter(Boolean).join(" ")} lang={lang} disabled={disabled || loading} aria-busy={loading || undefined} aria-pressed={ariaPressed} aria-label={ariaLabel} onClick={onClick}>{children}</button>;
}

export function FormField({ label, error, children }: { label: string; error?: string; children: ReactNode }) {
  return <label className="drl-form-field"><span>{label}</span>{children}{error && <small className="drl-form-field__error" role="alert">{error}</small>}</label>;
}

export function parseMoneyE4(value: bigint | string): bigint | undefined {
  const text = typeof value === "bigint" ? value.toString() : value;
  if (!/^\d+$/.test(text)) return undefined;
  try {
    const amount = BigInt(text);
    return amount % 100n === 0n ? amount : undefined;
  } catch {
    return undefined;
  }
}

export function formatMoneyE4(
  value: bigint | string,
  { invalidLabel, currencySymbol, language }: { invalidLabel: string; currencySymbol: string; language: Language }
): string {
  const amount = parseMoneyE4(value);
  if (amount === undefined) return invalidLabel;
  const whole = amount / 10_000n;
  const cents = (amount / 100n) % 100n;
  const groupedWhole = new Intl.NumberFormat(language).format(whole);
  return `${currencySymbol}${groupedWhole}.${cents.toString().padStart(2, "0")}`;
}

export function formatMoneyE4WithI18n(
  value: bigint | string,
  { language, translate }: { language: Language; translate: (key: TranslationKey) => string }
): string {
  return formatMoneyE4(value, {
    language,
    currencySymbol: translate("common.currencyUsd"),
    invalidLabel: translate("common.invalidAmount")
  });
}

type LegacyMoneyProps = {
  value: bigint | string;
  invalidLabel: string;
  currencySymbol: string;
  language: Language;
};

type LocalizedMoneyProps = {
  value: bigint | string;
  translate: (key: TranslationKey) => string;
};

function documentLanguage(): Language {
  if (typeof document === "undefined") return "en";
  const value = document.documentElement.lang.toLowerCase().split(/[-_]/)[0];
  return value === "zh" || value === "km" ? value : "en";
}

export function Money(props: LegacyMoneyProps | LocalizedMoneyProps) {
  const { value, invalidLabel, currencySymbol, language } = "translate" in props
    ? { value: props.value, invalidLabel: props.translate("common.invalidAmount"), currencySymbol: props.translate("common.currencyUsd"), language: documentLanguage() }
    : props;
  const amount = parseMoneyE4(value);
  if (amount === undefined) return <span className="drl-money drl-money--error" role="alert">{invalidLabel}</span>;
  return <span className="drl-money" data-e4={amount.toString()}>{formatMoneyE4(value, { invalidLabel, currencySymbol, language })}</span>;
}

export function MoneyWithI18n({
  value,
  language,
  translate
}: {
  value: bigint | string;
  language: Language;
  translate: (key: TranslationKey) => string;
}) {
  return <Money value={value} translate={translate} />;
}

export function TieredPrice({ tiers, invalidLabel, currencySymbol, language }: { tiers: readonly { label: string; value: bigint | string }[]; invalidLabel: string; currencySymbol: string; language: Language }) {
  return <ul className="drl-tiered-price">{tiers.map((tier) => <li key={tier.label}><span>{tier.label}</span><Money value={tier.value} invalidLabel={invalidLabel} currencySymbol={currencySymbol} language={language} /></li>)}</ul>;
}

export function ProductCard({ imageUrl, imageAlt, name, price, badge, onClick }: { imageUrl?: string; imageAlt: string; name: string; price: ReactNode; badge?: ReactNode; onClick?: () => void }) {
  const content = <><ImagePlaceholder src={imageUrl} alt={imageAlt} /><span className="drl-product-card__body"><h3>{name}</h3><span>{price}</span></span>{badge && <Badge>{badge}</Badge>}</>;
  return onClick ? <button type="button" className="drl-product-card" onClick={onClick}>{content}</button> : <article className="drl-product-card">{content}</article>;
}

export function Tabs({ tabs, activeId, onChange, ariaLabel }: { tabs: readonly { id: string; label: string }[]; activeId: string; onChange: (id: string) => void; ariaLabel: string }) {
  const tabRefs = React.useRef<Array<HTMLButtonElement | null>>([]);
  const handleKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const offset = event.key === "ArrowRight" || event.key === "ArrowDown" ? 1 : event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1 : event.key === "Home" ? -index : event.key === "End" ? tabs.length - index - 1 : 0;
    if (!offset || tabs.length === 0) return;
    event.preventDefault();
    const nextIndex = (index + offset + tabs.length) % tabs.length;
    onChange(tabs[nextIndex]!.id);
    tabRefs.current[nextIndex]?.focus();
  };
  return <div className="drl-tabs" role="tablist" aria-label={ariaLabel}>{tabs.map((tab, index) => <button ref={(element) => { tabRefs.current[index] = element; }} key={tab.id} type="button" role="tab" aria-selected={tab.id === activeId} tabIndex={tab.id === activeId ? 0 : -1} onKeyDown={(event) => handleKeyDown(event, index)} onClick={() => onChange(tab.id)}>{tab.label}</button>)}</div>;
}

export function Toast({ children, role = "status" }: { children: ReactNode; role?: "status" | "alert" }) {
  return <div className="drl-toast" role={role} aria-live={role === "status" ? "polite" : undefined}>{children}</div>;
}

export function Skeleton({ width = "100%", height = "1em" }: { width?: string; height?: string }) {
  return <span className="drl-skeleton" aria-hidden="true" style={{ width, height }} />;
}

export function ImagePlaceholder({ src, alt }: { src?: string; alt: string }) {
  const [failed, setFailed] = React.useState(false);
  if (!src || failed) return <span className="drl-image-placeholder" role="img" aria-label={alt} />;
  return <img className="drl-image-placeholder" src={src} alt={alt} onError={() => setFailed(true)} />;
}

export function Badge({ children }: { children: ReactNode }) {
  return <span className="drl-badge">{children}</span>;
}
