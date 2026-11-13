import { describe, expect, it } from "vitest";
import { adapters, maskSensitive } from "./api";

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
});
