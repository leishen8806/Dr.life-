import type { KeyboardEvent, ReactNode } from "react";

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
          key={index}
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
      {actionLabel && onAction && <button type="button" onClick={onAction}>{actionLabel}</button>}
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
