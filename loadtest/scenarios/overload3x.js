import http from "k6/http";
import { check } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

import { assert2xx, assertHealthz, assertReadyz, assertRetryAfter, verifyAll } from "../lib/checks.js";
import { validateTargetEnv } from "../lib/guard.js";
import { requireRoute } from "../lib/k6-preflight.js";
import { parseDurationSeconds, parsePositiveInt, requirePath } from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const heavyPath = requirePath(__ENV.OVERLOAD_HEAVY_PATH || "/v1/public/ping", "OVERLOAD_HEAVY_PATH");
const criticalPath = requirePath(__ENV.OVERLOAD_CRITICAL_PATH || "/readyz", "OVERLOAD_CRITICAL_PATH");
const healthPath = requirePath(__ENV.HEALTH_PATH || "/healthz", "HEALTH_PATH");
const vus = parsePositiveInt(__ENV, "OVERLOAD_VUS", "120");
const durationSeconds = parseDurationSeconds(__ENV, "OVERLOAD_DURATION", "60s");
if (vus < 2) {
  throw new Error("OVERLOAD_VUS must be at least 2 for a 3x overload run");
}

const rejectionDuration = new Trend("rejection_duration", true);
const criticalOk = new Rate("critical_ok");
const healthOk = new Rate("health_ok");
const heavyRejections = new Counter("heavy_rejections");
const heavyRetryAfterMissing = new Counter("heavy_retry_after_missing");

export const options = {
  vus,
  duration: `${durationSeconds}s`,
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    rejection_duration: ["p(95)<200"],
    critical_ok: ["rate>0.99"],
    health_ok: ["rate>0.99"],
    heavy_rejections: ["count>0"],
    heavy_retry_after_missing: ["count==0"],
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
  requireRoute(target.baseUrl + criticalPath, "overload critical", [200], assertReadyz);
  requireRoute(target.baseUrl + healthPath, "overload health", [200], assertHealthz);
  requireRoute(target.baseUrl + heavyPath, "overload heavy", [200, 503], function (res) {
    if (res.status === 200) {
      return;
    }
    if (res.status === 503) {
      assertRetryAfter(res, "overload heavy");
      return;
    }
    throw new Error(`unexpected heavy preflight status ${res.status}`);
  });
  return { startedAt: new Date().toISOString() };
}

export default function () {
  const heavy = http.get(target.baseUrl + heavyPath, {
    responseCallback: http.expectedStatuses(200, 503),
    tags: { kind: "heavy" },
  });
  if (heavy.status === 503) {
    heavyRejections.add(1);
    rejectionDuration.add(heavy.timings.duration);
    try {
      assertRetryAfter(heavy, "overload heavy");
    } catch (error) {
      heavyRetryAfterMissing.add(1);
      console.error(error.message);
    }
  }
  record(heavy, "heavy", [["heavy status", (res) => res.status === 200 || res.status === 503]]);

  const critical = http.get(target.baseUrl + criticalPath, {
    responseCallback: http.expectedStatuses(200),
    tags: { kind: "critical" },
  });
  criticalOk.add(critical.status === 200);
  record(critical, "critical", [["critical accepted", assertReadyz]]);

  const health = http.get(target.baseUrl + healthPath, {
    responseCallback: http.expectedStatuses(200),
    tags: { kind: "health" },
  });
  healthOk.add(health.status === 200);
  record(health, "health", [["health contract", assertHealthz]]);
}