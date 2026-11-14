import { Drawer, Space, Typography } from "antd";
import { useI18n, type TranslationKey } from "@drlife/i18n";

const productLabel = (product: string, t: (key: TranslationKey) => string) => product === "Ceramic tile" || product === "product.ceramicTile" ? t("wms.product.ceramicTile") : product === "LED panel" || product === "product.ledPanel" ? t("wms.product.ledPanel") : product;

export function DetailDrawer({ record, onClose }: { record: Record<string, string | number> | null; onClose: () => void }) {
  const { t } = useI18n();
  const labels: Record<string, string> = { id: t("wms.id"), product: t("wms.product"), zone: t("wms.zone"), location: t("wms.location"), quantity: t("wms.quantity"), available: t("wms.available"), locationTotal: t("wms.locationTotal"), locations: t("wms.locations"), movement: t("wms.movement"), reference: "deliveryMethod" in (record ?? {}) ? t("wms.pickTask") : t("wms.reference"), scheduled: t("wms.scheduled"), measured: t("wms.measured"), volume: t("wms.volume"), recommendedLocation: t("wms.putaway"), status: t("wms.status"), items: t("wms.items"), deliveryMethod: t("wms.deliveryMethod"), driver: t("wms.driver"), phone: t("wms.phone") };
  const values: Record<string, string> = { zto: t("wms.zto"), inhouse: t("wms.inhouse"), ready: t("wms.status.ready"), "pending-confirmation": t("wms.needsMerchantConfirm"), packed: t("wms.status.packed"), dispatched: t("wms.status.dispatched") };
  const displayValue = (key: string, value: string | number) => key === "phone" ? `****${String(value).replace(/^\*+/, "").slice(-4)}` : key === "product" ? productLabel(String(value), t) : values[String(value)] ?? String(value);
  return <Drawer title={t("wms.detail")} open={Boolean(record)} onClose={onClose} width={460}>{record ? <Space direction="vertical" size="middle" className="detail-stack">{Object.entries(record).filter(([key]) => !(key === "phone" && "product" in record)).map(([key, value]) => <Typography.Paragraph key={key} className="detail-row"><strong>{labels[key] ?? key}</strong><span>{displayValue(key, value)}</span></Typography.Paragraph>)}</Space> : null}</Drawer>;
}
