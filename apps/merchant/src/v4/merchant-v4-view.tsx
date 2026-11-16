import { useEffect, useMemo, useState } from "react";
import type { Language, TranslationKey } from "@drlife/i18n";
import { Button, Card, EmptyState, ErrorState, Loading, Money } from "@drlife/ui";
import type { MerchantDemand, MerchantV4Api, MerchantV4Profile, WarehouseSnapshot } from "./merchant-v4-api";

type Translate = (key: TranslationKey, params?: Record<string, string | number>) => string;
type Resource<T> = { status: "loading" } | { status: "error" } | { status: "ready"; value: T };

export function MerchantV4View({ api, language, t, onBack }: { api: MerchantV4Api; language: Language; t: Translate; onBack: () => void }) {
  const [profile, setProfile] = useState<Resource<MerchantV4Profile>>({ status: "loading" });
  const [warehouse, setWarehouse] = useState<Resource<WarehouseSnapshot>>({ status: "loading" });
  const [section, setSection] = useState<"profile" | "demands" | "warehouse">("profile");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => { setProfile({ status: "loading" }); setWarehouse({ status: "loading" }); Promise.all([api.loadProfile(language), api.loadWarehouse(language)]).then(([nextProfile, nextWarehouse]) => { setProfile({ status: "ready", value: nextProfile }); setWarehouse({ status: "ready", value: nextWarehouse }); }, () => { setProfile({ status: "error" }); setWarehouse({ status: "error" }); }); }, [api, language, attempt]);
  if (profile.status === "loading" || warehouse.status === "loading") return <main className="mini-shell"><Loading label={t("common.loading")} skeleton /></main>;
  if (profile.status === "error" || warehouse.status === "error") return <main className="mini-shell"><ErrorState title={t("common.failure")} description={t("common.networkError")} retryLabel={t("common.retry")} onRetry={() => setAttempt((value) => value + 1)} /></main>;
  const hasFinance = profile.value.permissions.includes("merchant.finance.view");
  return <main className="mini-shell merchant-v4"><Button variant="secondary" onClick={onBack}>{t("common.back")}</Button><header className="merchant-v4__hero"><p>{t("merchant.profileLabel")}</p><h1>{profile.value.company}</h1><span>{profile.value.user}</span></header><nav className="merchant-v4__tabs" aria-label={t("merchant.sections")}><button type="button" aria-pressed={section === "profile"} onClick={() => setSection("profile")}>{t("nav.profile")}</button><button type="button" aria-pressed={section === "demands"} onClick={() => setSection("demands")}>{t("merchant.demands")}</button><button type="button" aria-pressed={section === "warehouse"} onClick={() => setSection("warehouse")}>{t("merchant.warehouse")}</button></nav>{section === "profile" ? <MerchantProfile profile={profile.value} hasFinance={hasFinance} language={language} t={t} onDemand={() => setSection("demands")} /> : section === "demands" ? <DemandBoard demands={profile.value.demands} api={api} language={language} t={t} /> : <WarehouseView warehouse={warehouse.value} language={language} t={t} />}</main>;
}

export function MerchantProfile({ profile, hasFinance, language, t, onDemand }: { profile: MerchantV4Profile; hasFinance: boolean; language: Language; t: Translate; onDemand: () => void }) {
  return <>{hasFinance ? <><Card><h2>{t("merchant.funds")}</h2><div className="merchant-v4__funds"><Money value={String(profile.availableE4)} language={language} translate={t} /><small>{t("merchant.available")}</small><span>{t("merchant.pending")}: <Money value={String(profile.pendingE4)} language={language} translate={t} /></span></div></Card><FinanceLedger entries={profile.financeEntries ?? []} language={language} t={t} /></> : null}<nav className="merchant-v4__entries"><button type="button" onClick={onDemand}>{t("merchant.demands")}</button>{hasFinance ? <button type="button" disabled title={t("common.comingSoon")}>{t("merchant.finance")}</button> : null}<button type="button" disabled title={t("common.comingSoon")}>{t("merchant.staff")}</button><button type="button" disabled title={t("common.comingSoon")}>{t("merchant.help")}</button></nav></>;
}

function FinanceLedger({ entries, language, t }: { entries: NonNullable<MerchantV4Profile["financeEntries"]>; language: Language; t: Translate }) {
  const [filter, setFilter] = useState<"all" | "settlement" | "withdrawal">("all");
  const [page, setPage] = useState(1);
  const visible = entries.filter((entry) => filter === "all" || entry.type === filter).slice(0, page * 10);
  return <Card><div className="profile-card-heading"><h2>{t("merchant.finance")}</h2><Button variant="secondary" disabled title={t("common.comingSoon")}>{t("merchant.withdraw")}</Button></div><div className="merchant-v4__filters">{(["all", "settlement", "withdrawal"] as const).map((value) => <button type="button" key={value} aria-pressed={filter === value} onClick={() => { setFilter(value); setPage(1); }}>{t(`merchant.financeFilter.${value}` as TranslationKey)}</button>)}</div>{visible.length === 0 ? <EmptyState title={t("merchant.noFinanceEntries")} /> : <ul className="merchant-v4__demand-list">{visible.map((entry) => <li key={entry.id}><span><strong>{t(`merchant.financeType.${entry.type}` as TranslationKey)}</strong><small>{entry.detail} · {new Date(entry.createdAt).toLocaleString(language)}</small></span><Money value={String(entry.amountE4)} language={language} translate={t} /></li>)}</ul>}{visible.length < entries.filter((entry) => filter === "all" || entry.type === filter).length ? <Button variant="secondary" onClick={() => setPage((value) => value + 1)}>{t("wallet.loadMore")}</Button> : null}</Card>;
}

const metricKeys = ["awaitingPayment", "awaitingShipment", "inDelivery", "needQuote"] as const;
type MetricKey = (typeof metricKeys)[number];

const metricStates: Partial<Record<MetricKey, MerchantDemand["state"]>> = {
  awaitingPayment: "awaitingPayment",
  awaitingShipment: "awaitingShipment",
  inDelivery: "inDelivery",
  needQuote: "open"
};

export function getDemandMetricCount(demands: MerchantDemand[], key: MetricKey): number | undefined {
  const state = metricStates[key];
  if (!state || !demands.some((demand) => demand.state === state)) return undefined;
  return demands.filter((demand) => demand.state === state).length;
}

function DemandBoard({ demands, api, language, t }: { demands: MerchantDemand[]; api: MerchantV4Api; language: Language; t: Translate }) {
  const [filter, setFilter] = useState<"all" | MerchantDemand["state"]>("all");
  const [active, setActive] = useState<MerchantDemand>();
  const visible = useMemo(() => demands.filter((demand) => filter === "all" || demand.state === filter), [demands, filter]);
  return <section className="merchant-v4__section"><div className="merchant-v4__subtabs" role="tablist"><button type="button" disabled title={t("common.comingSoon")}>{t("merchant.ordersTab")}</button><button type="button" aria-pressed>{t("merchant.demandsTab")}</button></div><h2>{t("merchant.demands")}</h2><div className="merchant-v4__metrics">{metricKeys.map((key) => { const count = getDemandMetricCount(demands, key); return count === undefined ? null : <button type="button" key={key} onClick={() => setFilter(metricStates[key] ?? "all")}><strong>{count}</strong><span>{t(`merchant.metric.${key}` as TranslationKey)}</span></button>; })}</div><div className="merchant-v4__filters">{(["all", "open", "quoted", "selected", "awaitingPayment", "awaitingShipment", "inDelivery"] as const).map((value) => <button type="button" key={value} aria-pressed={filter === value} onClick={() => setFilter(value)}>{t(`merchant.filter.${value}` as TranslationKey)}</button>)}</div>{visible.length === 0 ? <EmptyState title={t("merchant.noDemands")} /> : <ul className="merchant-v4__demand-list">{visible.map((demand) => <li key={demand.id}><button type="button" onClick={() => setActive(demand)}><span><strong>{demand.title}</strong><small>{t("merchant.quantity")}: {demand.quantity}</small></span><Money value={String(demand.budgetE4)} language={language} translate={t} /></button></li>)}</ul>}{active ? <QuotePanel demand={active} api={api} t={t} onClose={() => setActive(undefined)} /> : null}</section>;
}

function parseDollarsToE4(value: string): number | undefined {
  const normalized = value.trim().replace(/,/g, "");
  if (!/^\d+(?:\.\d{1,2})?$/.test(normalized)) return undefined;
  const [whole, cents = ""] = normalized.split(".");
  return Number(`${whole}${cents.padEnd(2, "0")}00`);
}

export function canSubmitQuote(isDev: boolean): boolean { return isDev; }

function QuotePanel({ demand, api, t, onClose }: { demand: MerchantDemand; api: MerchantV4Api; t: Translate; onClose: () => void }) {
  const [amount, setAmount] = useState(""); const [days, setDays] = useState("7"); const [validity, setValidity] = useState("7"); const [note, setNote] = useState(""); const [error, setError] = useState(false); const [sent, setSent] = useState(false);
  const available = canSubmitQuote(import.meta.env.DEV);
  const submit = async () => { if (!available) return; const amountE4 = parseDollarsToE4(amount); const deliveryDays = Number(days); const quoteValidity = Number(validity); if (amountE4 === undefined || !Number.isInteger(deliveryDays) || deliveryDays <= 0 || !Number.isInteger(quoteValidity) || quoteValidity <= 0) { setError(true); return; } setError(false); await api.submitQuote(demand.id, amountE4, deliveryDays, note); setSent(true); };
  return <Card><div className="profile-card-heading"><h3>{demand.title}</h3><Button variant="secondary" onClick={onClose}>{t("common.close")}</Button></div>{!available ? <p role="status">{t("merchant.quoteUnavailable")}</p> : sent ? <p role="status">{t("merchant.quoteSent")}</p> : <div className="merchant-v4__quote"><label>{t("merchant.quoteAmount")}<input inputMode="decimal" value={amount} onChange={(event) => setAmount(event.target.value)} /></label><label>{t("merchant.deliveryDays")}<input inputMode="numeric" value={days} onChange={(event) => setDays(event.target.value)} /></label><label>{t("merchant.quoteValidity")}<input inputMode="numeric" value={validity} onChange={(event) => setValidity(event.target.value)} /></label><label>{t("merchant.note")}<textarea value={note} onChange={(event) => setNote(event.target.value)} /></label>{error ? <p role="alert">{t("common.invalidAmount")}</p> : null}<Button variant="primary" onClick={() => void submit()}>{t("merchant.submitQuote")}</Button></div>}</Card>;
}

function WarehouseView({ warehouse, language, t }: { warehouse: WarehouseSnapshot; language: Language; t: Translate }) {
  return <section className="merchant-v4__section"><h2>{t("merchant.warehouse")}</h2><div className="merchant-v4__warehouse"><Card><strong><Money value={String(warehouse.rateE4PerCbm)} language={language} translate={t} /></strong><span>{t("merchant.storageRate")}</span></Card><Card><strong>{warehouse.volumeCbm} m³ / {warehouse.pieces}</strong><span>{t("merchant.inventory")}</span></Card><Card><strong>{warehouse.measuredCbm === undefined ? t("merchant.notMeasured") : `${warehouse.measuredCbm} m³`}</strong><span>{t("merchant.measured")}</span></Card><Card><strong><Money value={String(warehouse.storageFeeE4)} language={language} translate={t} /></strong><span>{t("merchant.storageFee")}</span></Card></div><InboundBooking t={t} /></section>;
}

function InboundBooking({ t }: { t: Translate }) {
  const [method, setMethod] = useState("self"); const [quantity, setQuantity] = useState("1"); const [submitted, setSubmitted] = useState(false);
  return <Card><h3>{t("merchant.inboundBooking")}</h3><label>{t("merchant.inboundProduct")}<input defaultValue="Product" /></label><label>{t("merchant.inboundQuantity")}<input inputMode="numeric" value={quantity} onChange={(event) => setQuantity(event.target.value)} /></label><label>{t("merchant.inboundMethod")}<select value={method} onChange={(event) => setMethod(event.target.value)}><option value="self">{t("merchant.inboundSelf")}</option><option value="pickup">{t("merchant.inboundPickup")}</option><option value="china">{t("merchant.inboundChina")}</option></select></label><Button variant="primary" disabled={!Number.isInteger(Number(quantity)) || Number(quantity) <= 0} onClick={() => setSubmitted(true)}>{t("merchant.submitInbound")}</Button>{submitted ? <p role="status">{t("merchant.inboundDone")}</p> : null}</Card>;
}
