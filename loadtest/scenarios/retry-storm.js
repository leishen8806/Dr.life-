import http from "k6/http";
import { sleep } from "k6";
import { Trend } from "k6/metrics";

import { validateTargetEnv } from "../lib/guard.js";
import { requireRoute } from "../lib/k6-preflight.js";
import { parseDurationSeconds, parsePositiveInt, requirePath } from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const probePath = requirePath(__ENV.RETRY_PROBE_PATH || "/v1/public/ping", "RETRY_PROBE_PATH");
const pressurePath = requirePath(__ENV.RETRY_PRESSURE_PATH || "/v1/public/reports/summary", "RETRY_PRESSURE_PATH");
const pressureVUs = parsePositiveInt(__ENV, "RETRY_PRESSURE_VUS", "80");
if (pressureVUs < 2) {
  throw new Error("RETRY_PRESSURE_VUS must be at least 2");
}
const pressureSeconds = parseDurationSeconds(__ENV, "RETRY_PRESSURE_DURATION", "30s");
const attempts = parsePositiveInt(__ENV, "RETRY_ATTEMPTS", "6");

const immediateElapsed = new Trend("retry_immediate_elapsed", true);
const backoffElapsed = new Trend("retry_backoff_elapsed", true);

export const options = {
  scenarios: {
    pressure: {
      executor: "constant-vus",
      vus: pressureVUs,
      duration: `${pressureSeconds}s`,
      exec: "pressure",
    },
    immediate: {
      executor: "per-vu-iterations",
      vus: 10,
      iterations: 20,
      startTime: "2s",
      exec: "immediate",
    },
    backoff: {
      executor: "per-vu-iterations",
      vus: 10,
      iterations: 20,
      startTime: "2s",
      exec: "backoff",
    },
  },
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    retry_immediate_elapsed: ["p(95)<5000"],
    retry_backoff_elapsed: ["p(95)<5000"],
  },
};

export function setup() {
  requireRoute(target.baseUrl + probePath, "retry probe", [200], null);
  requireRoute(target.baseUrl + pressurePath, "retry pressure", [200, 503], null);
  return { startedAt: new Date().toISOString() };
}

export function pressure() {
  http.get(target.baseUrl + pressurePath, {
    responseCallback: http.expectedStatuses(200, 503),
    tags: { kind: "pressure" },
  });
}

function retry(kind) {
  const started = Date.now();
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const res = http.get(target.baseUrl + probePath, {
      responseCallback: http.expectedStatuses(200, 429, 503),
      tags: { kind: "retry", strategy: kind },
    });
    if (res.status === 200) {
      break;
    }
    if (kind === "immediate") {
      sleep(0.01);
    } else {
      const base = 0.05 * Math.pow(2, attempt);
      const jitter = base * 0.2 * Math.random();
      sleep(base + jitter);
    }
  }
  const elapsed = Date.now() - started;
  if (kind === "immediate") {
    immediateElapsed.add(elapsed);
  } else {
    backoffElapsed.add(elapsed);
  }
}

export function immediate() {
  retry("immediate");
}

export function backoff() {
  retry("backoff");
}