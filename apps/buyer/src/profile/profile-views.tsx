import { useMemo, useState } from "react";
import type { TranslationKey, Language } from "@drlife/i18n";
import { Button, Card, EmptyState, ErrorState, LanguageSwitcher, Loading, Money } from "@drlife/ui";
import type { Resource } from "../resource";
import type { BuyerProfile, WalletEntry, WalletEntryType } from "./profile-api";

type Translate = (key: TranslationKey, params?: Record<string, string | number>) => string;

function SignedMoney({ amountE4, language, t }: { amountE4: number; language: Language; t: Translate }) {
  return <span>{amountE4 < 0 ? "−" : "+"}<Money value={String(Math.abs(amountE4))} language={language} translate={t} /></span>;
}

export function BuyerProfileView({ profile, language, t, onOpenWallet, onOpenOrders, onRetry }: {
  profile: Resource<BuyerProfile>;
  language: Language;
  t: Translate;
  onOpenWallet: () => void;
  onOpenOrders: () => void;
  onRetry: () => void;
}) {
  if (profile.status === "loading") return <main className="buyer-page"><Loading label={t("common.loading")} skeleton /></main>;
  if (profile.status === "error") return <main className="buyer-page"><ErrorState title={t("common.failure")} description={t(profile.messageKey)} retryLabel={t("common.retry")} onRetry={onRetry} /></main>;
  return <main className="buyer-page"><section className="buyer-page__body profile-page">
    <header className="profile-hero"><p className="profile-eyebrow">{t("profile.buyerLabel")}</p><h1>{profile.value.displayName}</h1><p>{profile.value.username}</p></header>
    <section className="profile-stats" aria-label={t("profile.ordersTitle")}>
      <button type="button" onClick={onOpenOrders}><strong>{profile.value.orderCounts.pending}</strong><span>{t("profile.pendingOrders")}</span></button>
      <button type="button" onClick={onOpenOrders}><strong>{profile.value.orderCounts.completed}</strong><span>{t("profile.completedOrders")}</span></button>
    </section>
    <Card><div className="profile-card-heading"><h2>{t("wallet.title")}</h2><Button variant="secondary" onClick={onOpenWallet}>{t("profile.viewWallet")}</Button></div><div className="wallet-summary"><Money value={String(profile.value.availableE4)} language={language} translate={t} /><span>{t("wallet.available")}</span><small>{t("wallet.frozen")}: <Money value={String(profile.value.frozenE4)} language={language} translate={t} /></small></div></Card>
    <nav className="profile-entry-list" aria-label={t("profile.entriesTitle")}>
      <button type="button" onClick={onOpenWallet}><span>{t("wallet.title")}</span><span aria-hidden="true">›</span></button>
      <a href="/privacy" target="_blank" rel="noreferrer">{t("consent.privacy")} ↗</a><a href="/terms" target="_blank" rel="noreferrer">{t("consent.terms")} ↗</a>
      <button type="button" disabled title={t("common.comingSoon")}><span>{t("profile.purchaseOrders")}</span><span aria-hidden="true">›</span></button><button type="button" disabled title={t("common.comingSoon")}><span>{t("profile.help")}</span><span aria-hidden="true">›</span></button><LanguageSwitcher />
    </nav>
  </section></main>;
}

const filters: (WalletEntryType | "all")[] = ["all", "payment", "refund", "topup", "withdrawal"];

export function BuyerWalletView({ entries, language, t, onBack, onRetry }: { entries: Resource<WalletEntry[]>; language: Language; t: Translate; onBack: () => void; onRetry: () => void }) {
  const [filter, setFilter] = useState<WalletEntryType | "all">("all");
  const [selected, setSelected] = useState<WalletEntry>();
  const [page, setPage] = useState(1);
  const filtered = useMemo(() => entries.status === "ready" ? entries.value.filter((entry) => filter === "all" || entry.type === filter) : [], [entries, filter]);
  const visible = filtered.slice(0, page * 10);
  const changeFilter = (next: WalletEntryType | "all") => { setFilter(next); setPage(1); };
  return <main className="buyer-page"><section className="buyer-page__body wallet-page"><Button variant="secondary" onClick={onBack}>{t("common.back")}</Button><h1>{t("wallet.title")}</h1><div className="wallet-actions"><Button variant="secondary" disabled title={t("common.comingSoon")}>{t("wallet.topup")}</Button><Button variant="secondary" disabled title={t("common.comingSoon")}>{t("wallet.withdrawal")}</Button></div><div className="wallet-filters" role="tablist">{filters.map((value) => <button type="button" role="tab" aria-selected={filter === value} key={value} onClick={() => changeFilter(value)}>{t(`wallet.filter.${value}` as TranslationKey)}</button>)}</div>{entries.status === "loading" ? <Loading label={t("common.loading")} skeleton /> : entries.status === "error" ? <ErrorState title={t("common.failure")} description={t(entries.messageKey)} retryLabel={t("common.retry")} onRetry={onRetry} /> : visible.length === 0 ? <EmptyState title={t("wallet.empty")} /> : <><ul className="wallet-list">{visible.map((entry) => <li key={entry.id}><button type="button" onClick={() => setSelected(entry)}><span>{t(`wallet.type.${entry.type}` as TranslationKey)}<small>{new Date(entry.createdAt).toLocaleString(language)}</small></span><strong className={entry.amountE4 < 0 ? "wallet-negative" : "wallet-positive"}><SignedMoney amountE4={entry.amountE4} language={language} t={t} /></strong></button></li>)}</ul>{visible.length < filtered.length ? <Button variant="secondary" onClick={() => setPage((value) => value + 1)}>{t("wallet.loadMore")}</Button> : null}</>}{selected ? <Card><div className="profile-card-heading"><h2>{t("wallet.details")}</h2><Button variant="secondary" onClick={() => setSelected(undefined)}>{t("common.close")}</Button></div><dl className="wallet-details"><dt>{t("wallet.typeLabel")}</dt><dd>{t(`wallet.type.${selected.type}` as TranslationKey)}</dd><dt>{t("wallet.amount")}</dt><dd><SignedMoney amountE4={selected.amountE4} language={language} t={t} /></dd><dt>{t("wallet.time")}</dt><dd>{new Date(selected.createdAt).toLocaleString(language)}</dd><dt>{t("wallet.order")}</dt><dd>{selected.orderPublicId ?? t("wallet.notLinked")}</dd></dl></Card> : null}</section></main>;
}
