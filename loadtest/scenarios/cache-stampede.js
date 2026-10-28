import http from "k6/http";
import { check } from "k6";
import { Trend } from "k6/metrics";

import { assert2xx, verifyAll } from "../lib/checks.js";
import { validateMetricsUrl, validateTargetEnv } from "../lib/guard.js";
import { requireMetricsEndpoint, requireRoute } from "../lib/k6-preflight.js";
import { PreflightError, requireMetric } from "../lib/preflight.js";
import { scrapeMetrics } from "../lib/observability.js";
import { parsePositiveInt, requirePath } from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const cachePath = requirePath(__ENV.CACHE_PROBE_PATH || "/v1/public/catalog/hot", "CACHE_PROBE_PATH");
const originMetric = String(__ENV.CACHE_ORIGIN_METRIC || "drlife_cache_origin_total").trim();
const vus = parsePositiveInt(__ENV, "CACHE_STAMPEDE_VUS", "1000");
const iterations = parsePositiveInt(__ENV, "CACHE_STAMPEDE_ITERATIONS", "1000");

if (originMetric === "") {
  throw new PreflightError("CACHE_ORIGIN_METRIC is required");
}

const cacheOriginDelta = new Trend("cache_origin_delta", true);

export const options = {
  scenarios: {
    stampede: {
      executor: "shared-iterations",
      vus,
      iterations,
      maxDuration: "60s",
    },
  },
  thresholds: {
    checks: ["rate>0.99"],
    cache_origin_delta: ["p(95)<=1"],
  },
};

function record(res, label, assertions) {
  for (const result of verifyAll(res, label, assertions)) {
    check(res, { [result.name]: () => result.passed });
    if (!result.passed) {
      console.error(`${result.name}: ${result.error}`);
    }
  }
}

export function setup() {
  const metrics = validateMetricsUrl(__ENV, target);
  requireMetricsEndpoint(metrics.url, [originMetric]);
  requireRoute(target.baseUrl + cachePath, "cache probe", [200], assert2xx);
  const before = scrapeMetrics(metrics.url);
  return { metricsUrl: metrics.url, before: requireMetric(before, originMetric) };
}

export default function () {
  const res = http.get(target.baseUrl + cachePath, {
    responseCallback: http.expectedStatuses(200),
    tags: { kind: "cache" },
  });
  record(res, "cache probe", [["cache hit contract", assert2xx]]);
}

export function teardown(data) {
  const after = scrapeMetrics(data.metricsUrl);
  const delta = requireMetric(after, originMetric) - data.before;
  cacheOriginDelta.add(delta);
  if (delta > 1) {
    throw new PreflightError(`cache stampede: origin requests increased by ${delta}, expected at most 1`);
  }
}