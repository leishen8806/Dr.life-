import { readFileSync } from "node:fs";
import { join } from "node:path";
const html = readFileSync(join("dist", "assets", "index-zphCq-CN.js"), "utf8");
for (const marker of ["Ceramic tile", "ASN-24001", "PK-24001", "wms.inventory.read"]) if (html.includes(marker)) throw new Error(`production bundle contains development marker: ${marker}`);
console.log("production bundle contains no WMS development markers");
