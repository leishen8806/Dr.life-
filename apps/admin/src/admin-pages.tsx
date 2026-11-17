import { useEffect, useState } from "react";
import { Alert, Button, Form, Input, Modal, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useI18n } from "@drlife/i18n";
import { api } from "./api";
import type { AdminUserRecord, AuditRecord } from "./api/types";

const roles = ["super", "ops", "finance", "logi", "cs", "auditor"];

export function AdminManagementPage() {
  const { t } = useI18n();
  const [rows, setRows] = useState<AdminUserRecord[]>([]); const [open, setOpen] = useState(false); const [credentials, setCredentials] = useState<{ password: string; uri: string }>(); const [error, setError] = useState(false);
  const reload = () => api.listAdmins().then((result) => setRows(result.items)).catch(() => setError(true));
  useEffect(() => { reload(); }, []);
  const create = async (values: { name: string; email: string; role: string; dataScope: string; totpCode: string }) => { try { const stepUpToken = await api.stepUp("permission_change", values.totpCode); const result = await api.createAdmin({ name: values.name, email: values.email, role: values.role, dataScope: values.dataScope.split(",").map((item) => item.trim()).filter(Boolean), stepUpToken }); setCredentials({ password: result.initialPassword, uri: result.totpProvisioningUri }); setOpen(false); await reload(); } catch { setError(true); } };
  const columns: ColumnsType<AdminUserRecord> = [
    { title: t("admin.name"), dataIndex: "name" }, { title: t("admin.email"), dataIndex: "email" }, { title: t("admin.role"), dataIndex: "role" }, { title: t("admin.scope"), dataIndex: "dataScope", render: (scope: string[]) => scope.join(", ") }, { title: t("admin.status"), dataIndex: "status", render: (status: string) => <Tag color={status === "active" ? "green" : "default"}>{t(`admin.${status}` as never)}</Tag> },
    { title: t("admin.details"), key: "actions", render: (_value, row) => <Button onClick={async () => { const token = await api.stepUp("permission_change", window.prompt(t("admin.actionRequiresCode")) ?? ""); if (row.status === "active") await api.disableAdmin(row.publicId, "admin action", token); else await api.enableAdmin(row.publicId, token); await reload(); }}>{row.status === "active" ? t("admin.disable") : t("admin.enable")}</Button> }
  ];
  return <div className="admin-page"><Space direction="vertical" size="large" style={{ width: "100%" }}><Space style={{ justifyContent: "space-between", width: "100%" }}><Typography.Title level={2}>{t("admin.administrators")}</Typography.Title><Button type="primary" onClick={() => setOpen(true)}>{t("admin.createAdmin")}</Button></Space>{error && <Alert type="error" message={t("admin.error")} />}<Table rowKey="publicId" columns={columns} dataSource={rows} pagination={{ pageSize: 10 }} /></Space>
    <Modal open={open} title={t("admin.createAdmin")} footer={null} onCancel={() => setOpen(false)}><Form layout="vertical" initialValues={{ role: "ops", dataScope: "" }} onFinish={create}><Form.Item name="name" label={t("admin.name")} rules={[{ required: true }]}><Input /></Form.Item><Form.Item name="email" label={t("admin.email")} rules={[{ required: true, type: "email" }]}><Input /></Form.Item><Form.Item name="role" label={t("admin.role")} rules={[{ required: true }]}><Select options={roles.map((role) => ({ value: role, label: role }))} /></Form.Item><Form.Item name="dataScope" label={t("admin.scope")}><Input placeholder="KH-PP" /></Form.Item><Form.Item name="totpCode" label={t("admin.login.totp")} rules={[{ required: true, pattern: /^[0-9]{6,8}$/ }]}><Input inputMode="numeric" /></Form.Item><Button type="primary" htmlType="submit">{t("admin.createAdmin")}</Button></Form></Modal>
    <Modal open={Boolean(credentials)} title={t("admin.createAdmin")} footer={<Button onClick={() => setCredentials(undefined)}>{t("admin.saveCredentials")}</Button>} onCancel={() => setCredentials(undefined)}><Alert type="warning" message={t("admin.oneTimeOnly")} /><Typography.Paragraph><strong>{t("admin.initialPassword")}:</strong> {credentials?.password}</Typography.Paragraph><Typography.Paragraph><strong>{t("admin.totpUri")}:</strong> {credentials?.uri}</Typography.Paragraph></Modal>
  </div>;
}

export function AuditPage() {
  const { t } = useI18n(); const [rows, setRows] = useState<AuditRecord[]>([]); const [error, setError] = useState(false);
  useEffect(() => { api.listAudit().then((result) => setRows(result.items)).catch(() => setError(true)); }, []);
  const columns: ColumnsType<AuditRecord> = [{ title: "ID", dataIndex: "id" }, { title: t("admin.createdAt"), dataIndex: "ts" }, { title: t("admin.role"), dataIndex: "actorRole" }, { title: t("admin.audit"), dataIndex: "action" }, { title: t("admin.target" as never), dataIndex: "target" }, { title: t("admin.requestId" as never), dataIndex: "requestId" }];
  return <div className="admin-page"><Typography.Title level={2}>{t("admin.audit")}</Typography.Title>{error && <Alert type="error" message={t("admin.error")} />}<Table rowKey="id" columns={columns} dataSource={rows} pagination={{ pageSize: 20 }} /></div>;
}
