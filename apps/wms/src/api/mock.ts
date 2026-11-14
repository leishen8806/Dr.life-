import type { Inventory, PickTask, Receiving, WmsApi } from "./types";
import { assignTask, packTask, receiveRecord } from "../view-model";

let inventories: Inventory[] = [{ id: "i-1", product: "product.ceramicTile", zone: "A", location: "A-01-02", quantity: 120, available: 90, locationTotal: 120, phone: "****1234", locations: "A-01-02: 120", movement: "IN-24001" }, { id: "i-2", product: "product.ledPanel", zone: "B", location: "B-03-01", quantity: 80, available: 80, locationTotal: 72, phone: "****5678", locations: "B-03-01: 72", movement: "OUT-24002" }];
let receiving: Receiving[] = [{ id: "r-1", reference: "ASN-24001", scheduled: 100, measured: 102, volume: 12, status: "pending-confirmation", recommendedLocation: "A-01-03" }];
let picks: PickTask[] = [{ id: "p-1", reference: "PK-24001", items: 5, deliveryMethod: "zto", driver: "Sokha", phone: "****2222", status: "ready" }];

export const mockApi: WmsApi = {
  inventory: { list: async () => inventories.map((row) => ({ ...row })), adjust: async (id, quantity) => { inventories = inventories.map((row) => row.id === id ? { ...row, quantity, available: Math.min(row.available, quantity) } : row); } },
  receiving: { list: async () => receiving.map((row) => ({ ...row })), receive: async (id, measured, volume, threshold = 0) => { receiving = receiving.map((row) => row.id === id ? receiveRecord(row, measured, volume, threshold) : row); } },
  dispatch: { list: async () => picks.map((row) => ({ ...row })), pack: async (id) => { picks = picks.map((row) => row.id === id ? packTask(row) : row); }, assign: async (id, driver, deliveryMethod) => { picks = picks.map((row) => row.id === id ? { ...assignTask(row, driver), deliveryMethod: deliveryMethod ?? row.deliveryMethod } : row); } }
};
