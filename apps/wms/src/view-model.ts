import type { PickTask, Receiving } from "./api";

export function canAdjust(permissions: string[], selected: boolean) {
  return permissions.includes("wms.inventory.adjust") && selected;
}

export function inventoryRowClass(quantity: number, locationTotal: number) {
  return quantity !== locationTotal ? "row-mismatch" : "";
}

export function canSubmitAdjustment(quantity: number | undefined, reason: string | undefined) {
  return typeof quantity === "number" && Number.isInteger(quantity) && quantity >= 0 && Boolean(reason?.trim());
}

export function receivingStatus(measured: number, scheduled: number, threshold = 0) {
  return Math.abs(measured - scheduled) > threshold ? "pending-confirmation" : "ready";
}

export function packTask(task: PickTask): PickTask {
  return task.status === "ready" ? { ...task, status: "packed" } : task;
}

export function assignTask(task: PickTask, driver: string): PickTask {
  return driver.trim() && task.status === "packed" ? { ...task, driver, status: "dispatched" } : task;
}

export function receiveRecord(record: Receiving, measured: number, volume: number, threshold = 0): Receiving {
  return { ...record, measured, volume, status: receivingStatus(measured, record.scheduled, threshold) };
}
