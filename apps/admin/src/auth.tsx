import { useEffect, useState, type ReactNode } from "react";
import { Alert, Button, Card, Form, Input, Space, Typography } from "antd";
import { applyDocumentLanguage, storeLanguagePreference, useI18n, type Language } from "@drlife/i18n";
import { AdminApiError, api } from "./api";

export function LoginPage({ onSuccess }: { onSuccess: (mustChangePassword: boolean) => void }) {
  const { t } = useI18n();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);
  const submit = async (values: { email: string; password: string; totpCode: string }) => {
    setLoading(true); setError(undefined);
    try { const result = await api.login(values); onSuccess(Boolean(result.mustChangePassword)); }
    catch (cause) { const apiError = cause as Partial<AdminApiError>; if (apiError.status === 429) { const seconds = Number(apiError.retryAfter ?? 0); setError(t("admin.login.rateLimited", { seconds: Number.isFinite(seconds) ? seconds : 0 })); } else setError(t("admin.login.failed")); }
    finally { setLoading(false); }
  };
  return <Card className="admin-auth-card" title={t("admin.login.title")}><Form layout="vertical" onFinish={submit} requiredMark={false}>
    {error && <Alert role="alert" type="error" showIcon message={error} />}
    <Form.Item label={t("admin.login.email")} name="email" rules={[{ required: true, type: "email" }]}><Input autoComplete="username" /></Form.Item>
    <Form.Item label={t("admin.login.password")} name="password" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item>
    <Form.Item label={t("admin.login.totp")} name="totpCode" rules={[{ required: true, pattern: /^[0-9]{6,8}$/ }]}><Input inputMode="numeric" autoComplete="one-time-code" maxLength={8} /></Form.Item>
    <Button type="primary" htmlType="submit" loading={loading} block>{t("admin.login.submit")}</Button>
  </Form></Card>;
}

export function PasswordChangePage({ onComplete }: { onComplete: () => void }) {
  const { t } = useI18n();
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(false);
  const submit = async (values: { currentPassword: string; newPassword: string; confirmPassword: string; totpCode: string }) => {
    setError(false); if (values.newPassword !== values.confirmPassword) { setError(true); return; }
    setLoading(true); try { await api.changePassword(values); onComplete(); } catch { setError(true); } finally { setLoading(false); }
  };
  return <Card className="admin-auth-card" title={t("admin.login.changePassword")}><Form layout="vertical" onFinish={submit}>
    {error && <Alert role="alert" type="error" message={t("admin.login.failed")} />}
    <Form.Item label={t("admin.login.password")} name="currentPassword" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item>
    <Form.Item label={t("admin.login.newPassword")} name="newPassword" rules={[{ required: true, min: 20 }]}><Input.Password autoComplete="new-password" /></Form.Item>
    <Form.Item label={t("admin.login.confirmPassword")} name="confirmPassword" rules={[{ required: true }]}><Input.Password autoComplete="new-password" /></Form.Item>
    <Form.Item label={t("admin.login.totp")} name="totpCode" rules={[{ required: true, pattern: /^[0-9]{6,8}$/ }]}><Input inputMode="numeric" autoComplete="one-time-code" /></Form.Item>
    <Button type="primary" htmlType="submit" loading={loading} block>{t("admin.login.savePassword")}</Button>
  </Form></Card>;
}

export function AuthGate({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const [state, setState] = useState<"loading" | "login" | "password" | "ready">(() => window.location.pathname === "/login" ? "login" : "loading");
  useEffect(() => {
    const expired = () => setState("login"); window.addEventListener("drlife-auth-expired", expired);
    if (window.location.pathname === "/login") return;
    if (import.meta.env.DEV && import.meta.env.VITE_ADMIN_SCREENSHOT === "true") { api.login({ email: "admin@example.test", password: "screenshot-only", totpCode: "123456" }).then(() => setState("ready")); return; }
    api.refresh().then(() => setState("ready")).catch(() => setState("login"));
    return () => window.removeEventListener("drlife-auth-expired", expired);
  }, []);
  if (state === "loading") return <div className="admin-auth-layout" aria-busy="true"><Typography.Text>{t("admin.loading")}</Typography.Text></div>;
  if (state === "login") return <div className="admin-auth-layout"><AuthLanguageSwitcher /><LoginPage onSuccess={(mustChange) => setState(mustChange ? "password" : "ready")} /></div>;
  if (state === "password") return <div className="admin-auth-layout"><PasswordChangePage onComplete={() => setState("login")} /></div>;
  return <AuthActions>{children}</AuthActions>;
}

export function AuthToolbar() { const { t } = useI18n(); return <Button onClick={() => { api.logout(); window.dispatchEvent(new Event("drlife-auth-expired")); }}>{t("admin.logout")}</Button>; }
function AuthActions({ children }: { children: ReactNode }) {
  return <div className="admin-authenticated">{children}</div>;
}

function AuthLanguageSwitcher() { const { language, setLanguage, t } = useI18n(); return <Space className="admin-auth-language"><Button onClick={() => { const next = language === "zh" ? "en" : language === "en" ? "km" : "zh"; storeLanguagePreference(next); applyDocumentLanguage(next); setLanguage(next as Language); }}>{t("language.select")}: {language}</Button></Space>; }
