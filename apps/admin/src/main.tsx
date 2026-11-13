import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { Alert, Button, DatePicker, Drawer, Empty, Input, Layout, Menu, Result, Segmented, Select, Skeleton, Space, Table, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { BrowserRouter, Link, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { applyDocumentLanguage, I18nProvider, readLanguagePreference, resolveLanguage, storeLanguagePreference, useI18n, type Language, type TranslationKey } from "@drlife/i18n";
import { Money, StatusTag } from "@drlife/ui";
import { api } from "./api";
import type { OrderRecord, RecordStatus, ReviewRecord } from "./api/types";
import "antd/dist/reset.css";
import "@drlife/ui/styles.css";
import "./styles.css";

const { Header, Sider, Content } = Layout;
const canUseMockWorkspace = import.meta.env.DEV;
const permissions: string[] = canUseMockWorkspace ? ["admin.review", "admin.orders", "admin.merchants", "admin.products"] : [];
const hasPermission = (permission: string) => permissions.includes(permission);

function keyText(t: (key: TranslationKey) => string, key: string) {
  return t(key as TranslationKey);
}

function AdminLanguageSwitcher() {
  const { language, setLanguage, t } = useI18n();
  const options = [{ value: "zh", label: t("language.zh") }, { value: "en", label: t("language.en") }, { value: "km", label: t("language.km") }];
  return <Segmented aria-label={t("language.select")} value={language} options={options} onChange={(value) => { const next = value as Language; storeLanguagePreference(next); applyDocumentLanguage(next); setLanguage(next); }} />;
}

function toneFor(status: string): "neutral" | "success" | "warning" | "danger" {
  if (status === "approved" || status === "normal") return "success";
  if (status === "rejected" || status === "exception") return "danger";
  if (status === "pending" || status === "attention") return "warning";
  return "neutral";
}

function StatusCell({ status }: { status: string }) {
  const { t } = useI18n();
  return <StatusTag tone={toneFor(status)}>{keyText(t, "admin." + status)}</StatusTag>;
}

function Overview() {
  const { t } = useI18n();
  const [counts, setCounts] = useState({ merchants: 0, orders: 0, review: 0 });
  useEffect(() => {
    if (!canUseMockWorkspace) return;
    Promise.all([api.listReviews(), api.listOrders()]).then(([reviews, orders]) => {
      setCounts({ merchants: reviews.merchants.length, orders: orders.length, review: reviews.merchants.length + reviews.products.length });
    });
  }, []);
  return <div className="admin-page">
    <Typography.Title level={2}>{t("admin.overview")}</Typography.Title>
    {canUseMockWorkspace && <div className="admin-cards" aria-label={t("admin.overview")}>
      <article className="admin-card"><strong>{t("admin.merchants")}</strong><span>{counts.merchants}</span></article>
      <article className="admin-card"><strong>{t("admin.orders")}</strong><span>{counts.orders}</span></article>
      <article className="admin-card"><strong>{t("admin.review")}</strong><span>{counts.review}</span></article>
    </div>}
  </div>;
}

export function ReviewTable({ rows, canManage, onApprove, onReject, kind }: { rows: ReviewRecord[]; canManage: boolean; onApprove: (row: ReviewRecord) => void; onReject: (row: ReviewRecord) => void; kind: "merchant" | "product" }) {
  const { t } = useI18n();
  const columns: ColumnsType<ReviewRecord> = [
    { title: kind === "merchant" ? t("admin.merchantName") : t("admin.productName"), dataIndex: "nameKey", render: (value: string) => keyText(t, value) },
    { title: t("admin.status"), dataIndex: "status", render: (value: string) => <StatusCell status={value} /> },
    { title: t("admin.submittedAt"), dataIndex: "createdAt" },
    { title: t("admin.details"), key: "actions", render: (_value, row) => <Space wrap>
      <Tooltip title={!canManage ? t("admin.noPermission") : undefined}><span><Button type="primary" size="small" disabled={!canManage || row.status !== "pending"} onClick={() => onApprove(row)}>{t("admin.approve")}</Button></span></Tooltip>
      <Tooltip title={!canManage ? t("admin.noPermission") : undefined}><span><Button danger size="small" disabled={!canManage || row.status !== "pending"} onClick={() => onReject(row)}>{t("admin.reject")}</Button></span></Tooltip>
    </Space> }
  ];
  return <Table rowKey="id" columns={columns} dataSource={rows} pagination={{ pageSize: 8 }} locale={{ emptyText: <Empty description={t("admin.empty")} /> }} />;
}

export function RejectReasonForm({ onSubmit }: { onSubmit: (reason: string) => void }) {
  const { t } = useI18n();
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState(false);
  const submit = () => {
    if (!reason.trim()) { setReasonError(true); return; }
    onSubmit(reason.trim());
  };
  return <div className="admin-reject-dialog" role="dialog" aria-label={t("admin.reject")}>
    <Typography.Title level={4}>{t("admin.reject")}</Typography.Title>
    <Input.TextArea rows={4} value={reason} status={reasonError ? "error" : undefined} onChange={(event) => { setReason(event.target.value); setReasonError(false); }} placeholder={t("admin.rejectReason")} aria-label={t("admin.rejectReason")} />
    {reasonError && <div className="admin-form-error" role="alert">{t("admin.rejectRequired")}</div>}
    <Button type="primary" onClick={submit}>{t("admin.reject")}</Button>
  </div>;
}

function ReviewPage() {
  const { t } = useI18n();
  const [active, setActive] = useState<"merchant" | "product">("merchant");
  const [data, setData] = useState<{ merchants: ReviewRecord[]; products: ReviewRecord[] }>({ merchants: [], products: [] });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [selected, setSelected] = useState<ReviewRecord>();
  const [actionError, setActionError] = useState(false);
  const reload = () => {
    setLoading(true); setError(false); setActionError(false);
    api.listReviews().then(setData).catch(() => setError(true)).finally(() => setLoading(false));
  };
  // The adapter call is an external synchronization; the initial loading state is intentional.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(reload, []);
  const rows = active === "merchant" ? data.merchants : data.products;
  const approve = (row: ReviewRecord) => { api.approveReview(active, row.id).then(reload).catch(() => setActionError(true)); };
  const reject = (reason: string) => {
    if (!selected) return;
    api.rejectReview(active, selected.id, reason).then(() => {
      setSelected(undefined); reload();
    }).catch(() => setActionError(true));
  };
  return <div className="admin-page">
    <Typography.Title level={2}>{t("admin.review")}</Typography.Title>
    <div className="admin-tabs" role="tablist">
      <Button type={active === "merchant" ? "primary" : "default"} role="tab" aria-selected={active === "merchant"} onClick={() => setActive("merchant")}>{t("admin.reviewMerchant")}</Button>
      <Button type={active === "product" ? "primary" : "default"} role="tab" aria-selected={active === "product"} onClick={() => setActive("product")}>{t("admin.reviewProduct")}</Button>
    </div>
    {actionError && <Alert type="error" showIcon message={t("admin.error")} />}
    {!hasPermission("admin.review") && <Alert type="warning" showIcon message={t("admin.noPermission")} />}
    {loading ? <Skeleton active /> : error ? <Alert type="error" showIcon message={t("admin.error")} action={<Button onClick={reload}>{t("admin.retry")}</Button>} /> : <ReviewTable rows={rows} kind={active} canManage={hasPermission("admin.review")} onApprove={approve} onReject={setSelected} />}
    {selected && <RejectReasonForm onSubmit={reject} />}
  </div>;
}

export function OrderPage() {
  const { t } = useI18n();
  const [rows, setRows] = useState<OrderRecord[]>([]);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<RecordStatus>();
  const [dateRange, setDateRange] = useState<[string, string]>(["", ""]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [selected, setSelected] = useState<OrderRecord>();
  const reload = () => {
    setLoading(true); setError(false);
    api.listOrders({ query, status, from: dateRange[0] || undefined, to: dateRange[1] || undefined }).then(setRows).catch(() => setError(true)).finally(() => setLoading(false));
  };
  useEffect(() => {
    // Filters synchronize the view with the adapter and intentionally refresh loading state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    reload();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, status, dateRange]);
  const columns: ColumnsType<OrderRecord> = [
    { title: t("admin.orderNo"), dataIndex: "id" },
    { title: t("admin.buyer"), dataIndex: "buyerKey", render: (value: string) => keyText(t, value) },
    { title: t("admin.merchant"), dataIndex: "merchantKey", render: (value: string) => keyText(t, value) },
    { title: t("admin.amount"), dataIndex: "amountE4", align: "right", render: (value: string) => <Money value={value} /> },
    { title: t("admin.orderStatus"), dataIndex: "status", render: (value: string) => <StatusCell status={value} /> },
    { title: t("admin.createdAt"), dataIndex: "createdAt" },
    { title: t("admin.delivery"), dataIndex: "deliveryKey", render: (value: string) => keyText(t, value) }
  ];
  return <div className="admin-page">
    <Typography.Title level={2}>{t("admin.orders")}</Typography.Title>
    <Space wrap className="admin-filters">
      <Input allowClear value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("admin.search")} aria-label={t("admin.search")} />
      <Select allowClear value={status} onChange={setStatus} placeholder={t("admin.status")} options={[
        { value: "normal", label: t("admin.normal") }, { value: "attention", label: t("admin.attention") }, { value: "exception", label: t("admin.exception") }
      ]} />
      <DatePicker.RangePicker onChange={(_, dateStrings) => setDateRange(dateStrings as [string, string])} aria-label={t("admin.date")} />
    </Space>
    {loading ? <Skeleton active /> : error ? <Alert type="error" showIcon message={t("admin.error")} action={<Button onClick={reload}>{t("admin.retry")}</Button>} /> : <Table rowKey="id" columns={columns} dataSource={rows} pagination={{ pageSize: 8 }} onRow={(row) => ({ onClick: () => setSelected(row), className: "admin-clickable-row" })} locale={{ emptyText: <Empty description={t("admin.noResults")} /> }} />}
    <Drawer open={Boolean(selected)} title={t("admin.orderDetail")} onClose={() => setSelected(undefined)} width={440}>
      {selected && <div className="order-detail">
        <Typography.Paragraph><strong>{t("admin.orderNo")}:</strong> {selected.id}</Typography.Paragraph>
        <Typography.Paragraph><strong>{t("admin.buyer")}:</strong> {keyText(t, selected.buyerKey)}</Typography.Paragraph>
        <Typography.Paragraph><strong>{t("admin.merchant")}:</strong> {keyText(t, selected.merchantKey)}</Typography.Paragraph>
        <Typography.Paragraph><strong>{t("admin.amount")}:</strong> <Money value={selected.amountE4} /></Typography.Paragraph>
        <Typography.Paragraph><strong>{t("admin.createdAt")}:</strong> {selected.createdAt}</Typography.Paragraph>
        <Typography.Paragraph><strong>{t("admin.phone")}:</strong> {selected.phone}</Typography.Paragraph>
        <Typography.Title level={4}>{t("admin.suborders")}</Typography.Title>
        <ul>{selected.suborders.map((item) => <li key={item.id}>{item.id} · {keyText(t, item.productKey)} · <StatusCell status={item.status} /></li>)}</ul>
        <Typography.Paragraph><strong>{t("admin.delivery")}:</strong> {keyText(t, selected.deliveryKey)}</Typography.Paragraph>
      </div>}
    </Drawer>
  </div>;
}

function ProductionPlaceholder() {
  const { t } = useI18n();
  return <Result status="403" title={t("admin.noPermission")} subTitle={t("admin.loginUnavailable")} />;
}

function Shell() {
  const { t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  if (!canUseMockWorkspace) return <ProductionPlaceholder />;
  const items = [
    { key: "/", label: <Link to="/">{t("admin.overview")}</Link> },
    { key: "accounts", label: t("admin.accounts"), children: [
      ...(hasPermission("admin.orders") ? [{ key: "/orders", label: <Link to="/orders">{t("admin.orders")}</Link> }] : []),
      ...(hasPermission("admin.merchants") ? [{ key: "/merchants", label: <span>{t("admin.merchants")}</span>, disabled: true }] : []),
      ...(hasPermission("admin.products") ? [{ key: "/products", label: <span>{t("admin.products")}</span>, disabled: true }] : [])
    ] },
    { key: "operations", label: t("admin.operations"), children: hasPermission("admin.review") ? [{ key: "/review", label: <Link to="/review">{t("admin.review")}</Link> }] : [] }
  ];
  const selectedKey = location.pathname === "/" ? "/" : location.pathname;
  return <Layout className="admin-shell">
    <Sider breakpoint="lg" collapsedWidth="0" className="admin-sider">
      <div className="brand">Dr.life+</div>
      <Menu theme="dark" mode="inline" selectedKeys={[selectedKey]} items={items} onClick={({ key }) => navigate(key)} />
    </Sider>
    <Layout>
      <Header className="admin-header"><Typography.Title level={4}>{t("app.admin")} · {t("admin.operator")}</Typography.Title><AdminLanguageSwitcher /></Header>
      <Content className="admin-content">
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/review" element={canUseMockWorkspace ? <ReviewPage /> : <ProductionPlaceholder />} />
          <Route path="/orders" element={canUseMockWorkspace ? <OrderPage /> : <ProductionPlaceholder />} />
          <Route path="*" element={<Result status="404" title={t("shell.notFound")} />} />
        </Routes>
      </Content>
    </Layout>
  </Layout>;
}

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <I18nProvider initialLanguage={resolveLanguage(readLanguagePreference(), undefined)}>
        <BrowserRouter><Shell /></BrowserRouter>
      </I18nProvider>
    </StrictMode>
  );
}
