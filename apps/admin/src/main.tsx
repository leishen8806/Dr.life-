import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { Alert, Button, Drawer, Empty, Input, Layout, Menu, Modal, Result, Select, Skeleton, Space, Table, Tabs, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { BrowserRouter, Link, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { I18nProvider, readLanguagePreference, resolveLanguage, useI18n, type TranslationKey } from "@drlife/i18n";
import { LanguageSwitcher, Money, StatusTag } from "@drlife/ui";
import { api } from "./api";
import type { OrderRecord, RecordStatus, ReviewRecord } from "./api/types";
import "antd/dist/reset.css";
import "@drlife/ui/styles.css";
import "./styles.css";

const { Header, Sider, Content } = Layout;
const canUseMockWorkspace = import.meta.env.DEV;

function keyText(t: (key: TranslationKey) => string, key: string) {
  return t(key as TranslationKey);
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
  return <div className="admin-page">
    <Typography.Title level={2}>{t("admin.overview")}</Typography.Title>
    {canUseMockWorkspace && <div className="admin-cards" aria-label={t("admin.overview")}>
      <article className="admin-card"><strong>{t("admin.merchants")}</strong><span>2</span></article>
      <article className="admin-card"><strong>{t("admin.orders")}</strong><span>2</span></article>
      <article className="admin-card"><strong>{t("admin.review")}</strong><span>3</span></article>
    </div>}
  </div>;
}

function ReviewTable({ rows, onApprove, onReject }: { rows: ReviewRecord[]; onApprove: (row: ReviewRecord) => void; onReject: (row: ReviewRecord) => void }) {
  const { t } = useI18n();
  const columns: ColumnsType<ReviewRecord> = [
    { title: t("admin.merchants"), dataIndex: "nameKey", render: (value: string) => keyText(t, value) },
    { title: t("admin.status"), dataIndex: "status", render: (value: string) => <StatusCell status={value} /> },
    { title: t("admin.createdAt"), dataIndex: "createdAt" },
    { title: t("admin.details"), key: "actions", render: (_value, row) => <Space wrap>
      <Button type="primary" size="small" disabled={row.status !== "pending"} onClick={() => onApprove(row)}>{t("admin.approve")}</Button>
      <Button danger size="small" disabled={row.status !== "pending"} onClick={() => onReject(row)}>{t("admin.reject")}</Button>
    </Space> }
  ];
  return <Table rowKey="id" columns={columns} dataSource={rows} pagination={{ pageSize: 8 }} locale={{ emptyText: <Empty description={t("admin.empty")} /> }} />;
}

function ReviewPage() {
  const { t } = useI18n();
  const [active, setActive] = useState<"merchant" | "product">("merchant");
  const [data, setData] = useState<{ merchants: ReviewRecord[]; products: ReviewRecord[] }>({ merchants: [], products: [] });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [selected, setSelected] = useState<ReviewRecord>();
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState(false);
  const reload = () => {
    setLoading(true); setError(false);
    api.listReviews().then(setData).catch(() => setError(true)).finally(() => setLoading(false));
  };
  useEffect(reload, []);
  const rows = active === "merchant" ? data.merchants : data.products;
  const approve = (row: ReviewRecord) => { api.approveReview(active, row.id).then(reload); };
  const reject = () => {
    if (!selected || !reason.trim()) { setReasonError(true); return; }
    api.rejectReview(active, selected.id, reason.trim()).then(() => {
      setSelected(undefined); setReason(""); setReasonError(false); reload();
    });
  };
  return <div className="admin-page">
    <Typography.Title level={2}>{t("admin.review")}</Typography.Title>
    <Tabs activeKey={active} onChange={(value) => setActive(value as "merchant" | "product")} items={[
      { key: "merchant", label: t("admin.reviewMerchant"), children: null },
      { key: "product", label: t("admin.reviewProduct"), children: null }
    ]} />
    {loading ? <Skeleton active /> : error ? <Alert type="error" showIcon message={t("admin.error")} action={<Button onClick={reload}>{t("admin.retry")}</Button>} /> : <ReviewTable rows={rows} onApprove={approve} onReject={setSelected} />}
    <Modal open={Boolean(selected)} title={t("admin.reject")} okText={t("admin.reject")} onCancel={() => { setSelected(undefined); setReason(""); setReasonError(false); }} onOk={reject}>
      <Input.TextArea rows={4} value={reason} status={reasonError ? "error" : undefined} onChange={(event) => { setReason(event.target.value); setReasonError(false); }} placeholder={t("admin.rejectReason")} aria-label={t("admin.rejectReason")} />
      {reasonError && <div className="admin-form-error" role="alert">{t("admin.rejectRequired")}</div>}
    </Modal>
  </div>;
}

function OrderPage() {
  const { t } = useI18n();
  const [rows, setRows] = useState<OrderRecord[]>([]);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<RecordStatus>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [selected, setSelected] = useState<OrderRecord>();
  useEffect(() => {
    setLoading(true);
    api.listOrders({ query, status }).then(setRows).catch(() => setError(true)).finally(() => setLoading(false));
  }, [query, status]);
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
    </Space>
    {error ? <Alert type="error" showIcon message={t("admin.error")} /> : <Table rowKey="id" loading={loading} columns={columns} dataSource={rows} pagination={{ pageSize: 8 }} onRow={(row) => ({ onClick: () => setSelected(row), className: "admin-clickable-row" })} locale={{ emptyText: <Empty description={t("admin.noResults")} /> }} />}
    <Drawer open={Boolean(selected)} title={t("admin.orderDetail")} onClose={() => setSelected(undefined)} width={440}>
      {selected && <div className="order-detail">
        <Typography.Paragraph><strong>{t("admin.orderNo")}:</strong> {selected.id}</Typography.Paragraph>
        <Typography.Paragraph><strong>{t("admin.buyer")}:</strong> {keyText(t, selected.buyerKey)}</Typography.Paragraph>
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
  const items = [
    { key: "/", label: <Link to="/">{t("admin.overview")}</Link> },
    { key: "accounts", label: t("admin.accounts"), children: [
      { key: "/orders", label: <Link to="/orders">{t("admin.orders")}</Link> },
      { key: "/merchants", label: <span>{t("admin.merchants")}</span>, disabled: true }
    ] },
    { key: "operations", label: t("admin.operations"), children: [{ key: "/review", label: <Link to="/review">{t("admin.review")}</Link> }] }
  ];
  const selectedKey = location.pathname === "/" ? "/" : location.pathname;
  return <Layout className="admin-shell">
    <Sider breakpoint="lg" collapsedWidth="0" className="admin-sider">
      <div className="brand">Dr.life+</div>
      <Menu theme="dark" mode="inline" selectedKeys={[selectedKey]} items={items} onClick={({ key }) => navigate(key)} />
    </Sider>
    <Layout>
      <Header className="admin-header"><Typography.Title level={4}>{t("app.admin")} · {t("admin.operator")}</Typography.Title><LanguageSwitcher /></Header>
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

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider initialLanguage={resolveLanguage(readLanguagePreference(), undefined)}>
      <BrowserRouter><Shell /></BrowserRouter>
    </I18nProvider>
  </StrictMode>
);
