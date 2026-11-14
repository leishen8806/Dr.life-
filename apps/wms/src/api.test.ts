import { describe, expect, it } from "vitest";
import { t } from "@drlife/i18n";
import { adapters, buildCsv, maskSensitive } from "./api";
import { assignTask, canAdjust, canSubmitAdjustment, inventoryRowClass, packTask, receiveRecord } from "./view-model";

describe("WMS adapters", () => {
  it("keeps phone exports masked", () => expect(maskSensitive("0123456789")).toBe("******6789"));
  it("contains an intentionally inconsistent inventory sample", async () => {
    const rows = await adapters.inventory.list();
    expect(rows.some((row) => row.quantity !== row.locationTotal)).toBe(true);
  });
  it("marks receiving variance for merchant confirmation", async () => {
    const rows = await adapters.receiving.list();
    expect(rows.find((row) => row.measured !== row.scheduled)?.status).toBe("pending-confirmation");
  });
  it("marks mismatches for the view and rejects an empty adjustment reason", () => {
    expect(inventoryRowClass(8, 7)).toBe("row-mismatch");
    expect(canSubmitAdjustment(8, "")).toBe(false);
    expect(canSubmitAdjustment(8, "cycle count")).toBe(true);
  });
  it("moves receiving variance into merchant confirmation using the threshold", () => {
    const row = { id: "r", reference: "ASN", scheduled: 100, measured: 0, volume: 0, status: "ready" as const, recommendedLocation: "A" };
    expect(receiveRecord(row, 102, 12, 1).status).toBe("pending-confirmation");
  });
  it("moves dispatch tasks through pack and assign states", () => {
    const task = { id: "p", reference: "PK", items: 1, deliveryMethod: "zto" as const, driver: "", phone: "", status: "ready" as const };
    expect(packTask(task).status).toBe("packed");
    expect(assignTask(packTask(task), "Sokha").status).toBe("dispatched");
  });
  it("translates receiving and dispatch enum values in every supported language", () => {
    for (const language of ["zh", "en", "km"] as const) {
      expect(t("wms.needsMerchantConfirm", {}, language)).not.toBe("pending-confirmation");
      expect(t("wms.status.ready", {}, language)).not.toBe("ready");
      expect(t("wms.status.packed", {}, language)).not.toBe("packed");
      expect(t("wms.status.dispatched", {}, language)).not.toBe("dispatched");
      expect(t("wms.zto", {}, language)).not.toBe("zto");
      expect(t("wms.inhouse", {}, language)).not.toBe("inhouse");
    }
  });
  it("disables adjustment without the adjustment permission", () => {
    expect(canAdjust(["wms.inventory.read"], true)).toBe(false);
    expect(canAdjust(["wms.inventory.adjust"], true)).toBe(true);
  });
  it("builds masked and escaped CSV content", () => {
    const csv = buildCsv([{ phone: "0123456789", note: 'a, "quoted" value' }]);
    expect(csv).toContain('"******6789"');
    expect(csv).toContain('"a, ""quoted"" value"');
  });
});
