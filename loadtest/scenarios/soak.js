import http from "k6/http";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";

import { assert2xx, verifyAll } from "../lib/checks.js";
import { validateMetricsUrl, validateTargetEnv } from "../lib/guard.js";
import { requireMetricsEndpoint, requireRoute } from "../lib/k6-preflight.js";
import { scrapeMetrics } from "../lib/observability.js";
import { requireMetric } from "../lib/preflight.js";
import { evaluateTrend, parseDurationSeconds, requirePath } from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const soakPath = requirePath(__ENV.SOAK_PATH || "/v1/public/ping", "SOAK_PATH");
const durationSeconds = parseDurationSeconds(__ENV, "SOAK_DURATION", "10m");
const sampleSeconds = Number(String(__ENV.SOAK_SAMPLE_SECONDS || "30").trim());
const leakDetected = new Counter("soak_leak_detected");

if (!Number.isFinite(sampleSeconds) || sampleSeconds < 1) {
  throw new Error("SOAK_SAMPLE_SECONDS must be a positive number");
}
if (durationSeconds < sampleSeconds * 4) {
  throw new Error("SOAK_DURATION must cover at least four leak samples");
}

const goroutineSamples = [];
const memorySamples = [];

export const options = {
  scenarios: {
    soak: {
      executor: "constant-vus",
      vus: 1,
      duration: `${durationSeconds}s`,
    },
  },
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    soak_leak_detected: ["count==0"],
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
  requireMetricsEndpoint(metrics.url, ["go_goroutines", "process_resident_memory_bytes"]);
  requireRoute(target.baseUrl + soakPath, "soak probe", [200], assert2xx);
  return { metricsUrl: metrics.url };
}

export default function (data) {
  const res = http.get(target.baseUrl + soakPath, {
    responseCallback: http.expectedStatuses(200),
    tags: { kind: "soak" },
  });
  record(res, "soak probe", [["soak accepted", assert2xx]]);
  const metrics = scrapeMetrics(data.metricsUrl);
  const goroutines = requireMetric(metrics, "go_goroutines");
  const memory = requireMetric(metrics, "process_resident_memory_bytes");
  goroutineSamples.push(goroutines);
  memorySamples.push(memory);
  check({ goroutines, memory }, {
    "goroutines finite": () => Number.isFinite(goroutines),
    "memory finite": () => Number.isFinite(memory),
  });
  if (goroutineSamples.length >= 4) {
    try {
      evaluateTrend(goroutineSamples, { label: "goroutines", maxRatio: 1.5, minGrowth: 10 });
      evaluateTrend(memorySamples, { label: "memory", maxRatio: 1.25, minGrowth: 1024 * 1024 });
    } catch (error) {
      leakDetected.add(1);
      check({ leak: true }, { "no goroutine or memory leak": () => false });
      console.error(error.message);
    }
  }
  sleep(sampleSeconds);
}