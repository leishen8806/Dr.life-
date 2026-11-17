import { useMemo, useState, type ChangeEvent } from "react";
import { Button, Card, EmptyState, FormField, ListItem, NotificationList, StatusTag } from "@drlife/ui";
import type { Language, TranslationKey } from "@drlife/i18n";

type Translate = (key: TranslationKey, params?: Record<string, string | number>) => string;

const categories = ["design", "kitchen", "bathroom", "flooring", "lighting", "furniture"] as const;
const companies = [
  { id: "lotus", name: "Lotus Build", region: "Phnom Penh", specialties: ["design", "kitchen"], description: "Residential renovation and custom joinery.", products: ["Kitchen cabinet", "Oak flooring"] },
  { id: "mekong", name: "Mekong Home", region: "Kandal", specialties: ["bathroom", "lighting"], description: "Practical materials for family homes.", products: ["Wall tile", "LED panel"] }
];
const cases = [
  { id: "case-1", title: "Warm apartment", area: "82 m²", style: "Modern", company: "Lotus Build" },
  { id: "case-2", title: "Mekong riverside home", area: "146 m²", style: "Natural", company: "Mekong Home" }
];

export interface InquiryDraft { description: string; budget: string; duration: string; region: string; contact: "telegram" | "phone" | "email"; }
export function validateInquiry(draft: InquiryDraft, t: Translate): string[] {
  return [
    !draft.description.trim() ? t("validation.required") : "",
    !draft.budget.trim() ? t("validation.required") : "",
    !draft.duration.trim() ? t("validation.required") : "",
    !draft.region.trim() ? t("validation.required") : ""
  ].filter(Boolean);
}

export function DecorView({ t, onBack, onOpenNotifications }: { language?: Language; t: Translate; onBack: () => void; onOpenNotifications: () => void }) {
  const [tab, setTab] = useState<"companies" | "cases">("companies");
  const [query, setQuery] = useState("");
  const [region, setRegion] = useState("all");
  const [category, setCategory] = useState<string>();
  const [selectedCompany, setSelectedCompany] = useState<typeof companies[number]>();
  const [selectedCase, setSelectedCase] = useState<typeof cases[number]>();
  const [inquiry, setInquiry] = useState<InquiryDraft>({ description: "", budget: "", duration: "", region: "", contact: "telegram" });
  const [submitted, setSubmitted] = useState(false);
  const [inquiryAttempted, setInquiryAttempted] = useState(false);
  const [inquiryTouched, setInquiryTouched] = useState<Record<string, boolean>>({});
  const filteredCompanies = useMemo(() => companies.filter((item) => (!query || `${item.name} ${item.description}`.toLowerCase().includes(query.toLowerCase())) && (region === "all" || item.region === region) && (!category || item.specialties.includes(category as never))), [category, query, region]);
  const errors = validateInquiry(inquiry, t);
  const fieldErrors = [
    !inquiry.description.trim() ? t("validation.required") : "",
    !inquiry.budget.trim() ? t("validation.required") : "",
    !inquiry.duration.trim() ? t("validation.required") : "",
    !inquiry.region.trim() ? t("validation.required") : ""
  ];
  const inquiryFields = ["description", "budget", "duration", "region"];
  const visibleErrors = fieldErrors.map((error, index) => inquiryAttempted || inquiryTouched[inquiryFields[index]!] ? error : "");
  if (selectedCase) return <main className="buyer-page"><section className="buyer-page__body"><Button variant="text" onClick={() => setSelectedCase(undefined)}>{t("common.back")}</Button><Card><h1 className="buyer-section__title">{selectedCase.title}</h1><p>{selectedCase.area} · {selectedCase.style}</p><p>{selectedCase.company}</p><div className="v7-case-image" aria-label={t("buyer.caseImage")}>{t("buyer.caseImage")}</div></Card></section></main>;
  if (selectedCompany) return <main className="buyer-page"><section className="buyer-page__body"><Button variant="text" onClick={() => setSelectedCompany(undefined)}>{t("common.back")}</Button><Card><h1 className="buyer-section__title">{selectedCompany.name}</h1><p>{selectedCompany.description}</p><StatusTag tone="success">{selectedCompany.region}</StatusTag><h2>{t("buyer.products")}</h2><div className="v7-horizontal-list">{selectedCompany.products.map((product) => <Card key={product}><strong>{product}</strong><small>{t("buyer.available")}</small></Card>)}</div><h2>{t("buyer.inquiry")}</h2>{submitted ? <p role="status">{t("buyer.inquirySent")}</p> : <form onSubmit={(event) => { event.preventDefault(); setInquiryAttempted(true); if (!import.meta.env.DEV) return; if (!errors.length) setSubmitted(true); }}><FormField label={t("buyer.description")} error={visibleErrors[0]}><textarea value={inquiry.description} onBlur={() => setInquiryTouched({ ...inquiryTouched, description: true })} onChange={(event) => setInquiry({ ...inquiry, description: event.target.value })} /></FormField><FormField label={t("buyer.budget")} error={visibleErrors[1]}><input value={inquiry.budget} onBlur={() => setInquiryTouched({ ...inquiryTouched, budget: true })} onChange={(event) => setInquiry({ ...inquiry, budget: event.target.value })} /></FormField><FormField label={t("buyer.duration")} error={visibleErrors[2]}><input value={inquiry.duration} onBlur={() => setInquiryTouched({ ...inquiryTouched, duration: true })} onChange={(event) => setInquiry({ ...inquiry, duration: event.target.value })} /></FormField><FormField label={t("buyer.region")} error={visibleErrors[3]}><input value={inquiry.region} onBlur={() => setInquiryTouched({ ...inquiryTouched, region: true })} onChange={(event) => setInquiry({ ...inquiry, region: event.target.value })} /></FormField><fieldset><legend>{t("buyer.contact")}</legend>{(["telegram", "phone", "email"] as const).map((value) => <label key={value}><input type="radio" checked={inquiry.contact === value} onChange={() => setInquiry({ ...inquiry, contact: value })} />{t(`buyer.contact.${value}` as TranslationKey)}</label>)}</fieldset>{!import.meta.env.DEV && <p role="status">{t("common.comingSoon")}</p>}<Button type="submit" disabled={!import.meta.env.DEV}>{t("common.submit")}</Button></form>}</Card></section></main>;
  return <main className="buyer-page"><section className="buyer-page__body"><div className="v7-page-heading"><Button variant="text" onClick={onBack}>{t("common.back")}</Button><Button variant="secondary" onClick={onOpenNotifications}>{t("buyer.notifications")}</Button></div><div className="v7-tabs" role="tablist"><button type="button" aria-pressed={tab === "companies"} onClick={() => setTab("companies")}>{t("buyer.companies")}</button><button type="button" aria-pressed={tab === "cases"} onClick={() => setTab("cases")}>{t("buyer.cases")}</button></div>{tab === "companies" ? <><div className="v7-search-row"><input aria-label={t("common.search")} placeholder={t("common.search")} value={query} onChange={(event) => setQuery(event.target.value)} /><select aria-label={t("buyer.region")} value={region} onChange={(event) => setRegion(event.target.value)}><option value="all">{t("admin.all")}</option><option value="Phnom Penh">Phnom Penh</option><option value="Kandal">Kandal</option></select></div><div className="v7-category-grid">{categories.map((item) => <button type="button" key={item} aria-pressed={category === item} onClick={() => setCategory(category === item ? undefined : item)}>{t(`buyer.category.${item}` as TranslationKey)}</button>)}</div>{filteredCompanies.length ? filteredCompanies.map((item) => <Card key={item.id}><ListItem title={item.name} description={`${item.region} · ${item.description}`} trailing={t("buyer.viewCompany")} onClick={() => setSelectedCompany(item)} /></Card>) : <EmptyState title={t("common.empty")} />}</> : <div className="v7-case-grid">{cases.map((item) => <Card key={item.id}><button type="button" className="v7-card-button" onClick={() => setSelectedCase(item)}><div className="v7-case-image">{t("buyer.caseImage")}</div><strong>{item.title}</strong><small>{item.area} · {item.style}</small></button></Card>)}</div>}</section></main>;
}

export type ReceiptMethod = "photo" | "signature" | "code";
export function AftercareView({ t, onBack, delivered = true }: { t: Translate; onBack: () => void; delivered?: boolean }) {
  const [receipt, setReceipt] = useState<ReceiptMethod>();
  const [confirmed, setConfirmed] = useState(false);
  const [problemOpen, setProblemOpen] = useState(false);
  const [problem, setProblem] = useState({ type: "damage", description: "", files: [] as File[] });
  const [filePreviews, setFilePreviews] = useState<string[]>([]);
  const [sent, setSent] = useState(false);
  const canConfirm = delivered && !!receipt && !confirmed;
  const onFiles = (event: ChangeEvent<HTMLInputElement>) => { const files = Array.from(event.target.files ?? []).slice(0, 6); setProblem({ ...problem, files }); setFilePreviews(files.filter((file) => file.type.startsWith("image/")).map((file) => URL.createObjectURL(file))); };
  return <main className="buyer-page"><section className="buyer-page__body"><Button variant="text" onClick={onBack}>{t("common.back")}</Button><Card><h1 className="buyer-section__title">{t("buyer.orderAftercare")}</h1><StatusTag tone={confirmed ? "success" : "warning"}>{confirmed ? t("buyer.signed") : t("buyer.delivered")}</StatusTag>{delivered && !confirmed ? <><h2>{t("buyer.receiptProof")}</h2><div className="v7-choice-grid">{(["photo", "signature", "code"] as const).map((value) => <button type="button" key={value} aria-pressed={receipt === value} onClick={() => setReceipt(value)}>{t(`buyer.receipt.${value}` as TranslationKey)}</button>)}</div><Button disabled={!canConfirm} onClick={() => { if (import.meta.env.DEV) setConfirmed(true); }}>{t("buyer.confirmReceipt")}</Button></> : null}<Button variant="secondary" onClick={() => setProblemOpen(!problemOpen)}>{t("buyer.reportProblem")}</Button>{!import.meta.env.DEV && problemOpen && <p role="status">{t("common.comingSoon")}</p>}{problemOpen && import.meta.env.DEV && !sent ? <form onSubmit={(event) => { event.preventDefault(); if (problem.description.trim()) setSent(true); }}><FormField label={t("buyer.problemType")}><select value={problem.type} onChange={(event) => setProblem({ ...problem, type: event.target.value })}><option value="damage">{t("buyer.problem.damage")}</option><option value="missing">{t("buyer.problem.missing")}</option><option value="other">{t("buyer.problem.other")}</option></select></FormField><FormField label={t("buyer.description")}><textarea required value={problem.description} onChange={(event) => setProblem({ ...problem, description: event.target.value })} /></FormField><FormField label={t("buyer.photos")}><label className="buyer-upload-button"><span>{t("buyer.choosePhotos")}</span><input className="buyer-upload-input" type="file" accept="image/*" multiple onChange={onFiles} /></label><small>{problem.files.length}/6</small>{filePreviews.length > 0 && <div className="buyer-file-previews">{filePreviews.map((src) => <img key={src} src={src} alt={t("buyer.photos")} />)}</div>}</FormField><Button type="submit">{t("common.submit")}</Button></form> : null}{sent && <p role="status">{t("buyer.problemSent")}</p>}</Card></section></main>;
}

export interface NotificationItem { id: string; type: "order" | "wallet" | "product" | "warehouse" | "profile"; titleKey: TranslationKey; unread: boolean; target: string; }
export const mockNotifications: NotificationItem[] = [
  { id: "n1", type: "order", titleKey: "buyer.notice.order", unread: true, target: "order" },
  { id: "n2", type: "product", titleKey: "buyer.notice.product", unread: true, target: "profile" }
];
export function NotificationView({ t, onBack, onNavigate }: { language?: Language; t: Translate; onBack: () => void; onNavigate: (target: string) => void }) {
  const [items, setItems] = useState(mockNotifications);
  const unread = items.filter((item) => item.unread).length;
  return <main className="buyer-page"><section className="buyer-page__body"><Button variant="text" onClick={onBack}>{t("common.back")}</Button><Card><div className="v7-page-heading"><h1 className="buyer-section__title">{t("buyer.notifications")}</h1>{unread ? <StatusTag tone="warning">{unread}</StatusTag> : null}</div><NotificationList items={items.map((item) => ({ id: item.id, title: t(item.titleKey), description: item.unread ? t("buyer.unread") : t("buyer.read"), unread: item.unread }))} markAllLabel={t("buyer.markAllRead")} onMarkAllRead={() => setItems((current) => current.map((item) => ({ ...item, unread: false })))} onSelect={(id) => { const item = items.find((entry) => entry.id === id); setItems((current) => current.map((entry) => entry.id === id ? { ...entry, unread: false } : entry)); if (item) onNavigate(item.target); }} /></Card></section></main>;
}
