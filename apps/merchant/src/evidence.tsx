import { useState } from "react";
import { createRoot } from "react-dom/client";
import { t } from "@drlife/i18n";
import { Tabs } from "@drlife/ui";
import "@drlife/ui/styles.css";
import "./styles.css";
import { SuborderDetailView, SuborderListView } from "./orders/suborder-views";
import type { MerchantSuborder } from "./orders/suborders";

const order: MerchantSuborder = {
  public_id: "SO-20261114-0001",
  parent_public_id: "PO-20261114-0001",
  status: "shipped",
  items: [{ sku_public_id: "SKU-TILE-01", quantity: 2, unit_price_e4: 380000, line_total_e4: 760000 }],
  shipping_address_public_id: "ADDR-0001",
  shipping_address: { recipient_name: "Buyer F", address_text: "Phnom Penh", phone: "****1234" },
  goods_amount_e4: 760000,
  shipping_amount_e4: 0,
  payable_amount_e4: 760000,
  shipping_quote_expires_at: "2026-11-14T00:00:00Z",
  created_at: "2026-11-14T00:00:00Z",
  updated_at: "2026-11-14T00:00:00Z"
};

const orders = [order];
const tabs = ["all", "active", "shipped", "settled"].map((id) => ({ id, label: id[0]!.toUpperCase() + id.slice(1) }));

function App() {
  const params = new URLSearchParams(window.location.search);
  const language = (params.get("lang") === "zh" || params.get("lang") === "km" ? params.get("lang") : "en") as "zh" | "en" | "km";
  const [view, setView] = useState<"list" | "detail" | "tabs">(params.get("view") === "detail" ? "detail" : params.get("view") === "tabs" ? "tabs" : "list");
  const translate = (key: Parameters<typeof t>[0]) => t(key, {}, language);
  return <main className="merchant-evidence-shell">
    <header className="drl-app-header"><strong>Dr.life+ Merchant</strong><span>{language}</span></header>
    <nav className="merchant-evidence-nav" aria-label="Evidence views">
      <button type="button" onClick={() => setView("list")}>Orders</button>
      <button type="button" onClick={() => setView("tabs")}>Tabs</button>
    </nav>
    {view === "tabs" ? <Tabs tabs={tabs} activeId="all" onChange={() => undefined} ariaLabel="Order status" /> : view === "detail" ? <SuborderDetailView order={order} language={language} translate={translate} onBack={() => setView("list")} /> : <SuborderListView language={language} translate={translate} orders={orders} onSelectStatus={() => undefined} onOpen={() => setView("detail")} />}
  </main>;
}

createRoot(document.getElementById("root")!).render(<App />);
