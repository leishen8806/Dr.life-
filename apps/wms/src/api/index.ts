export * from "./types";
export * from "./export";
import type { WmsApi } from "./types";
import { exportCsv } from "./export";
import { realApi } from "./real";
export const adapters: WmsApi & { exportCsv: typeof exportCsv } = import.meta.env.DEV ? { ...(await import("./mock")).mockApi, exportCsv } : { ...realApi, exportCsv };
export async function loadApi(): Promise<WmsApi & { exportCsv: typeof exportCsv }> { if (import.meta.env.DEV) return { ...(await import("./mock")).mockApi, exportCsv }; return { ...realApi, exportCsv }; }
