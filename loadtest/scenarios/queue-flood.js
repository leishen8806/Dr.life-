import http from "k6/http";
import { check } from "k6";
import { Gauge, Trend } from "k6/metrics";

import { assert2xx, assertStatusIn, verifyAll } from "../lib/checks.js";
import { validateMetricsUrl, validateTargetEnv } from "../lib/guard.js";
import { requireBearerToken, requireMetricsEndpoint, requirePostRoute, requireRoute } from "../lib/k6-preflight.js";
import { requireMetric } from "../lib/preflight.js";
import { scrapeMetrics } from "../lib/observability.js";
import { parsePositiveInt, requirePath } from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const notifyPath = requirePath(__ENV.NOTIFY_PATH || "/v1/merchant/notifications", "NOTIFY_PATH");
const criticalPath = requirePath(__ENV.QUEUE_CRITICAL_PATH || "/v1/public/ping", "QUEUE_CRITICAL_PATH");
const tasks = parsePositiveInt(__ENV, "QUEUE_FLOOD_TASKS", "100000");
const vus = parsePositiveInt(__ENV, "QUEUE_FLOOD_VUS", "200");
if (vus < 2) {
  throw new Error("QUEUE_FLOOD_VUS must be at least 2");
}

const criticalDuration = new Trend("critical_duration", true);
const outboxLag = new Gauge("outbox_lag_seconds");

export const options = {
  scenarios: {
    flood: {
      executor: "shared-iterations",
      vus,
      iterations: tasks,
      maxDuration: String(__ENV.QUEUE_FLOOD_MAX_DURATION || "20m"),
      exec: "flood",
    },
    critical: {
      executor: "constant-vus",
      vus: 5,
      duration: String(__ENV.QUEUE_FLOOD_MAX_DURATION || "20m"),
      exec: "critical",
    },
  },
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    critical_duration: ["p(95)<1000"],
    outbox_lag_seconds: ["value<=60"],
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
  const token = requireBearerToken(__ENV);
  const headers = { Authorization: `Bearer ${token}`, "Content-Type": "application/json" };
  requireRoute(target.baseUrl + criticalPath, "queue critical", [200], assert2xx, headers);
  requirePostRoute(
    target.baseUrl + notifyPath,
    '{"template":"loadtest","payload":{"seq":1}}',
    "queue notify",
    [202, 400, 409, 422],
    { ...headers, "Idempotency-Key": `queue-preflight-${Date.now()}` },
    assertStatusIn,
  );
  const metrics = validateMetricsUrl(__ENV, target);
  requireMetricsEndpoint(metrics.url, ["drlife_outbox_lag_seconds"]);
  return { token, metricsUrl: metrics.url };
}

export function flood(data) {
  const key = `queue-${__VU}-${__ITER}-${Date.now()}`;
  const res = http.post(target.baseUrl + notifyPath, '{"template":"loadtest","payload":{"seq":1}}', {
    headers: {
      Authorization: `Bearer ${data.token}`,
      "Content-Type": "application/json",
      "Idempotency-Key": key,
    },
    responseCallback: http.expectedStatuses(202, 400, 409, 422),
    tags: { kind: "queue-flood" },
  });
  record(res, "queue notify", [["notify accepted", (r) => assertStatusIn(r, [202, 400, 409, 422], "queue notify")]]);
}

export function critical(data) {
  const res = http.get(target.baseUrl + criticalPath, {
    headers: { Authorization: `Bearer ${data.token}` },
    responseCallback: http.expectedStatuses(200),
    tags: { kind: "critical" },
  });
  criticalDuration.add(res.timings.duration);
  record(res, "queue critical", [["critical accepted", assert2xx]]);
}

export function teardown(data) {
  const metrics = scrapeMetrics(data.metricsUrl);
  outboxLag.add(requireMetric(metrics, "drlife_outbox_lag_seconds"));
}