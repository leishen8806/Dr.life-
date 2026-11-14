import type { WmsApi } from "./types";
// TODO(#245): bind these calls after W-09 confirms field names and OpenAPI paths.
export const realApi: WmsApi = { inventory: { list: async () => [], adjust: async () => undefined }, receiving: { list: async () => [], receive: async () => undefined }, dispatch: { list: async () => [], pack: async () => undefined, assign: async () => undefined } };
