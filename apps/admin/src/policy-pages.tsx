import { useEffect, useState } from "react";
import { Button, Drawer, Input, Segmented, Space, Table, Tooltip, Typography } from "antd";
import { useI18n } from "@drlife/i18n";
import { api } from "./api";
import type { AgreementRecord, IntegrationRecord } from "./api/types";

export function AgreementsPage() {
  const { t, language } = useI18n();
  const [rows, setRows] = useState<AgreementRecord[]>([]);
  const [selected, setSelected] = useState<AgreementRecord>();
  const [draftLanguage, setDraftLanguage] = useState<"zh" | "en" | "km">(language);
  const [changeNote, setChangeNote] = useState("");
  const [effectiveDate, setEffectiveDate] = useState("");
  useEffect(() => { api.listAgreements().then(setRows); }, []);
  const current = selected ?? rows[0];
  const complete = Boolean(current && Object.values(current.languages).every((value) => value.trim()) && changeNote.trim() && effectiveDate);
  const save = async () => {
    if (!current) return;
    const next = await api.saveAgreementDraft(current.id, draftLanguage, current.languages[draftLanguage]);
    setRows(rows.map((row) => row.id === next.id ? next : row)); setSelected(next);
  };
  const publish = async () => {
    if (!current || !complete) return;
    const next = await api.publishAgreement(current.id, { changeNote, effectiveDate });
    setRows(rows.map((row) => row.id === next.id ? next : row)); setSelected(next);
  };
  return <div className="admin-page">
    <Typography.Title level={2}>{t("admin.policies")}</Typography.Title>
    <Table rowKey="id" dataSource={rows} pagination={false} columns={[
      { title: t("admin.integrationName"), dataIndex: "kind", render: (kind: AgreementRecord["kind"]) => kind === "privacy" ? t("admin.policyPrivacy") : t("admin.policyTerms") },
      { title: t("admin.version"), dataIndex: "version" }, { title: t("admin.effectiveDate"), dataIndex: "effectiveDate" },
      { title: t("admin.coverage"), key: "coverage", render: (_: unknown, row: AgreementRecord) => `${row.consented}/${row.required}` },
      { title: t("admin.preview"), key: "preview", render: (_: unknown, row: AgreementRecord) => <Button onClick={() => setSelected(row)}>{t("admin.preview")}</Button> }
    ]} />
    {current && <Space direction="vertical" className="admin-form-grid">
      <Segmented value={draftLanguage} onChange={(value) => setDraftLanguage(value as "zh" | "en" | "km")} options={["zh", "en", "km"]} aria-label={t("language.select")} />
      <Input.TextArea rows={4} value={current.languages[draftLanguage]} onChange={(event) => setSelected({ ...current, languages: { ...current.languages, [draftLanguage]: event.target.value } })} aria-label={t("admin.policies")} />
      <Button onClick={save}>{t("admin.saveDraft")}</Button>
      <Input value={changeNote} onChange={(event) => setChangeNote(event.target.value)} placeholder={t("admin.changeNote")} aria-label={t("admin.changeNote")} />
      <Input type="date" value={effectiveDate} onChange={(event) => setEffectiveDate(event.target.value)} aria-label={t("admin.effectiveDate")} />
      <Tooltip title={!complete ? t("admin.publishConfirm") : undefined}><span><Button type="primary" disabled={!complete} onClick={publish}>{t("admin.publishVersion")}</Button></span></Tooltip>
      <Typography.Text type="secondary">{t("admin.auditServer")}</Typography.Text>
    </Space>}
  </div>;
}

export function IntegrationsPage() {
  const { t } = useI18n();
  const [rows, setRows] = useState<IntegrationRecord[]>([]);
  const [selected, setSelected] = useState<IntegrationRecord>();
  const [secret, setSecret] = useState("");
  useEffect(() => { api.listIntegrations().then(setRows); }, []);
  const refresh = () => api.listIntegrations().then(setRows);
  const test = async (row: IntegrationRecord) => { await api.testIntegration(row.id); refresh(); };
  const enable = async (row: IntegrationRecord) => { await api.enableIntegration(row.id); refresh(); };
  return <div className="admin-page">
    <Typography.Title level={2}>{t("admin.integrations")}</Typography.Title>
    <Table rowKey="id" dataSource={rows} pagination={false} columns={[
      { title: t("admin.integrationName"), dataIndex: "name" }, { title: t("admin.environment"), dataIndex: "environment" },
      { title: t("admin.endpoint"), dataIndex: "endpoint" }, { title: t("admin.secretLast4"), dataIndex: "secretLast4", render: (value: string) => `••••${value}` },
      { title: t("admin.lastTest"), key: "test", render: (_: unknown, row: IntegrationRecord) => row.lastTest ? `${row.lastTest.ok ? "✓" : "×"} ${row.lastTest.latencyMs}ms` : t("admin.notTested") },
      { title: t("admin.test"), key: "actions", render: (_: unknown, row: IntegrationRecord) => <Space><Button onClick={() => { setSelected(row); setSecret(""); }}>{t("admin.preview")}</Button><Button onClick={() => test(row)}>{t("admin.testConnection")}</Button><Tooltip title={!row.lastTest?.ok ? t("admin.notTested") : undefined}><span><Button disabled={!row.lastTest?.ok} onClick={() => enable(row)}>{t("admin.enable")}</Button></span></Tooltip></Space> }
    ]} />
    <Drawer open={Boolean(selected)} title={t("admin.integrations")} onClose={() => setSelected(undefined)}>
      {selected && <Space direction="vertical" style={{ width: "100%" }}><Input defaultValue={selected.endpoint} aria-label={t("admin.endpoint")} /><Input.Password value={secret} onChange={(event) => setSecret(event.target.value)} autoComplete="off" placeholder={t("admin.secretLast4")} aria-label={t("admin.secretLast4")} /><Button onClick={async () => { await api.updateIntegration(selected.id, { endpoint: selected.endpoint, environment: selected.environment, secret }); setSecret(""); await refresh(); }}>{t("admin.saveDraft")}</Button><Typography.Text type="secondary">{t("admin.auditServer")}</Typography.Text></Space>}
    </Drawer>
  </div>;
}
