import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Button, Layout, Menu, Result, Typography } from "antd";
import { BrowserRouter, Link, Route, Routes } from "react-router-dom";
import {
  I18nProvider,
  readLanguagePreference,
  resolveLanguage,
  useI18n
} from "@drlife/i18n";
import { LanguageSwitcher } from "@drlife/ui";
import "antd/dist/reset.css";
import "@drlife/ui/styles.css";
import "./styles.css";

const { Header, Sider, Content } = Layout;

function Placeholder() {
  const { t } = useI18n();
  return <Typography.Title level={2}>{t("shell.content")}</Typography.Title>;
}

function Forbidden() {
  const { t } = useI18n();
  return (
    <Result
      status="403"
      title="403"
      subTitle={t("shell.forbidden")}
      extra={
        <Button type="primary">
          <Link to="/">{t("common.back")}</Link>
        </Button>
      }
    />
  );
}

function NotFound() {
  const { t } = useI18n();
  return (
    <Result
      status="404"
      title="404"
      subTitle={t("shell.notFound")}
      extra={
        <Button type="primary">
          <Link to="/">{t("common.back")}</Link>
        </Button>
      }
    />
  );
}

function App() {
  const { t } = useI18n();
  return (
    <Layout className="admin-shell">
      <Sider breakpoint="lg" collapsedWidth="0" className="admin-sider">
        <div className="brand">Dr.life+</div>
        <Menu
          theme="dark"
          mode="inline"
          items={[
            {
              key: "placeholder",
              label: <Link to="/">{t("shell.menu")}</Link>
            },
            {
              key: "forbidden",
              label: <Link to="/403">{t("shell.forbidden")}</Link>
            }
          ]}
        />
      </Sider>
      <Layout>
        <Header className="admin-header">
          <Typography.Title level={4}>
            {t("app.wms")} · {t("shell.header")}
          </Typography.Title>
          <LanguageSwitcher />
        </Header>
        <Content className="admin-content">
          <Routes>
            <Route path="/" element={<Placeholder />} />
            <Route path="/403" element={<Forbidden />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </Content>
      </Layout>
    </Layout>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider
      initialLanguage={resolveLanguage(readLanguagePreference(), undefined)}
    >
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </I18nProvider>
  </StrictMode>
);
