import { useEffect, useMemo, useState } from "react";
import type { Language, TranslationKey } from "@drlife/i18n";
import { Button, Card, EmptyState, ErrorState, Loading, Money, parseMoneyE4 } from "@drlife/ui";
import type { MerchantDemand, MerchantV4Api, MerchantV4Profile, WarehouseSnapshot } from "./merchant-v4-api";

type Translate = (key: TranslationKey, params?: Record<string, string | number>) => string;
type Resource<T> = { status: "loading" } | { status: "error" } | { status: "ready"; value: T };

export function MerchantV4View({ api, language, t, onBack }: { api: MerchantV4Api; language: Language; t: Translate; onBack: () => void }) {
  const [profile, setProfile] = useState<Resource<MerchantV4Profile>>({ status: "loading" });
  const [warehouse, setWarehouse] = useState<Resource<WarehouseSnapshot>>({ status: "loading" });
  const [section, setSection] = useState<"profile" | "demands" | "warehouse">("profile");
  useEffect(() => { Promise.all([api.loadProfile(language), api.loadWarehouse(language)]).then(([nextProfile, nextWarehouse]) => { setProfile({ status: "ready", value: nextProfile }); setWarehouse({ status: "ready", value: nextWarehouse }); }, () => { setProfile({ status: "error" }); setWarehouse({ status: "error" }); }); }, [api, language]);
  if (profile.status === "loading" || warehouse.status === "loading") return <main className="mini-shell"><Loading label={t("common.loading")} skeleton /></main>;
  if (profile.status === "error" || warehouse.status === "error") return <main className="mini-shell"><ErrorState title={t("common.failure")} description={t("common.networkError")} retryLabel={t("common.retry")} onRetry={() => undefined} /></main>;
  const hasFinance = profile.value.permissions.includes("merchant.finance.view");
  return <main className="mini-shell merchant-v4"><Button variant="secondary" onClick={onBack}>{t("common.back")}</Button><header className="merchant-v4__hero"><p>{t("merchant.profileLabel")}</p><h1>{profile.value.company}</h1><span>{profile.value.user}</span></header><nav className="merchant-v4__tabs" aria-label={t("merchant.sections")}><button type="button" aria-pressed={section === "profile"} onClick={() => setSection("profile")}>{t("nav.profile")}</button><button type="button" aria-pressed={section === "demands"} onClick={() => setSection("demands")}>{t("merchant.demands")}</button><button type="button" aria-pressed={section === "warehouse"} onClick={() => setSection("warehouse")}>{t("merchant.warehouse")}</button></nav>{section === "profile" ? <MerchantProfile profile={profile.value} hasFinance={hasFinance} language={language} t={t} onDemand={() => setSection("demands")} /> : section === "demands" ? <DemandBoard demands={profile.value.demands} api={api} language={language} t={t} /> : <WarehouseView warehouse={warehouse.value} t={t} />}</main>;
}

function MerchantProfile({ profile, hasFinance, language, t, onDemand }: { profile: MerchantV4Profile; hasFinance: boolean; language: Language; t: Translate; onDemand: () => void }) {
  return <><Card><h2>{t("merchant.funds")}</h2><div className="merchant-v4__funds"><Money value={String(profile.availableE4)} language={language} translate={t} /><small>{t("merchant.available")}</small><span>{t("merchant.pending")}: <Money value={String(profile.pendingE4)} language={language} translate={t} /></span></div></Card><nav className="merchant-v4__entries"><button type="button" onClick={onDemand}>{t("merchant.demands")}</button>{hasFinance ? <button type="button" disabled>{t("merchant.finance")}</button> : null}<button type="button" disabled>{t("merchant.staff")}</button><button type="button" disabled>{t("merchant.help")}</button></nav></>;
}

function DemandBoard({ demands, api, language, t }: { demands: MerchantDemand[]; api: MerchantV4Api; language: Language; t: Translate }) {
  const [filter, setFilter] = useState<"all" | MerchantDemand["state"]>("all");
  const [active, setActive] = useState<MerchantDemand>();
  const visible = useMemo(() => demands.filter((demand) => filter === "all" || demand.state === filter), [demands, filter]);
  return <section className="merchant-v4__section"><h2>{t("merchant.demands")}</h2><div className="merchant-v4__metrics">{(["awaitingPayment", "awaitingShipment", "inDelivery", "needQuote"] as const).map((key, index) => <button type="button" key={key} onClick={() => setFilter(index === 3 ? "open" : "all")}><strong>{index + 1}</strong><span>{t(`merchant.metric.${key}` as TranslationKey)}</span></button>)}</div><div className="merchant-v4__filters">{(["all", "open", "quoted", "selected"] as const).map((value) => <button type="button" key={value} aria-pressed={filter === value} onClick={() => setFilter(value)}>{t(`merchant.filter.${value}` as TranslationKey)}</button>)}</div>{visible.length === 0 ? <EmptyState title={t("merchant.noDemands")} /> : <ul className="merchant-v4__demand-list">{visible.map((demand) => <li key={demand.id}><button type="button" onClick={() => setActive(demand)}><span><strong>{demand.title}</strong><small>{t("merchant.quantity")}: {demand.quantity}</small></span><Money value={String(demand.budgetE4)} language={language} translate={t} /></button></li>)}</ul>}{active ? <QuotePanel demand={active} api={api} t={t} onClose={() => setActive(undefined)} /> : null}</section>;
}

function QuotePanel({ demand, api, t, onClose }: { demand: MerchantDemand; api: MerchantV4Api; t: Translate; onClose: () => void }) {
  const [amount, setAmount] = useState(""); const [days, setDays] = useState("7"); const [note, setNote] = useState(""); const [error, setError] = useState(false); const [sent, setSent] = useState(false);
  const submit = async () => { const amountE4 = parseMoneyE4(amount); if (amountE4 === undefined) { setError(true); return; } setError(false); await api.submitQuote(demand.id, Number(amountE4), Number(days), note); setSent(true); };
  return <Card><div className="profile-card-heading"><h3>{demand.title}</h3><Button variant="secondary" onClick={onClose}>{t("common.close")}</Button></div>{sent ? <p role="status">{t("merchant.quoteSent")}</p> : <div className="merchant-v4__quote"><label>{t("merchant.quoteAmount")}<input inputMode="numeric" value={amount} onChange={(event) => setAmount(event.target.value)} /></label><label>{t("merchant.deliveryDays")}<input inputMode="numeric" value={days} onChange={(event) => setDays(event.target.value)} /></label><label>{t("merchant.note")}<textarea value={note} onChange={(event) => setNote(event.target.value)} /></label>{error ? <p role="alert">{t("common.invalidAmount")}</p> : null}<Button variant="primary" onClick={() => void submit()}>{t("merchant.submitQuote")}</Button></div>}</Card>;
}

function WarehouseView({ warehouse, t }: { warehouse: WarehouseSnapshot; t: Translate }) {
  return <section className="merchant-v4__section"><h2>{t("merchant.warehouse")}</h2><div className="merchant-v4__warehouse"><Card><strong>{warehouse.rateE4PerCbm / 10000} USD</strong><span>{t("merchant.storageRate")}</span></Card><Card><strong>{warehouse.volumeCbm} m³ / {warehouse.pieces}</strong><span>{t("merchant.inventory")}</span></Card><Card><strong>{warehouse.measuredCbm ?? t("merchant.notMeasured")} m³</strong><span>{t("merchant.measured")}</span></Card><Card><strong>{warehouse.storageFeeE4 / 10000} USD</strong><span>{t("merchant.storageFee")}</span></Card></div><Button variant="primary" disabled>{t("merchant.inboundBooking")}</Button></section>;
}
