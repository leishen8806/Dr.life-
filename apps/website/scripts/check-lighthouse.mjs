import { readFile } from "node:fs/promises";

const report = JSON.parse(await readFile(process.argv[2] ?? "lighthouse.json", "utf8"));
const score = (name) => Math.round((report.categories?.[name]?.score ?? 0) * 100);
const lcp = report.audits?.["largest-contentful-paint"]?.numericValue ?? Number.POSITIVE_INFINITY;
const checks = [
  ["performance", score("performance"), 80],
  ["accessibility", score("accessibility"), 90],
  ["LCP_ms", Math.round(lcp), 2500]
];
for (const [name, actual, minimum] of checks) {
  const pass = name === "LCP_ms" ? actual <= minimum : actual >= minimum;
  console.log(`${name}: ${actual} (threshold ${name === "LCP_ms" ? "<=" : ">="} ${minimum})`);
  if (!pass) process.exitCode = 1;
}
if (process.exitCode) throw new Error("Lighthouse acceptance gate failed");
