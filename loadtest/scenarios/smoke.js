import http from "k6/http";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";

import {
  assertHealthz,
  assertNoFingerprint,
  assertPermissionDenied,
  assertPublicPing,
  assertReadyz,
  assertSecurityHeaders,
  assertUnauthenticated,
  verifyAll,
} from "../lib/checks.js";
import { validateMetricsUrl, validateTargetEnv } from "../lib/guard.js";
import { requireMetricsEndpoint, requirePositiveInt, requireRoute } from "../lib/k6-preflight.js";
import { PreflightError } from "../lib/preflight.js";
import { probePaths } from "../lib/targets.js";

const target = validateTargetEnv(__ENV);
const paths = probePaths(__ENV);
const vus = requirePositiveInt(__ENV, "SMOKE_VUS", "1");
const duration = String(__ENV.SMOKE_DURATION || "10s").trim();
const mutationProof = new Counter("mutation_proof");

const routes = [
  {
    path: paths.health,
    label: "healthz",
    expected: [200],
    verify: assertHealthz,
  },
  {
    path: paths.ready,
    label: "readyz",
    expected: [200],
    verify: assertReadyz,
  },
  {
    path: paths.ping,
    label: "public ping",
    expected: [200],
    verify: assertPublicPing,
  },
  {
    path: paths.protectedProbe,
    label: "protected buyer route",
    expected: [401],
    verify: assertUnauthenticated,
  },
  {
    path: paths.unknownAudience,
    label: "unknown audience",
    expected: [403],
    verify: assertPermissionDenied,
  },
];

const thresholds = {
  checks: ["rate>0.99"],
  http_req_failed: ["rate<0.01"],
  http_req_duration: ["p(95)<500"],
};
if (__ENV.LOADTEST_MUTATION_PROOF === "1") {
  thresholds.mutation_proof = ["count>0"];
}

export const options = {
  vus,
  duration,
  thresholds,
};

export function setup() {
  for (const route of routes) {
    requireRoute(target.baseUrl + route.path, route.label, route.expected, route.verify);
  }
  const metricsRaw = String(__ENV.METRICS_URL || "").trim();
  if (metricsRaw !== "") {
    const metrics = validateMetricsUrl(__ENV, target);
    requireMetricsEndpoint(metrics.url, ["drlife_http_requests_total", "go_goroutines"]);
  }
  if (duration === "") {
    throw new PreflightError("SMOKE_DURATION must not be blank");
  }
  return { startedAt: new Date().toISOString() };
}

function record(res, label, assertions) {
  for (const result of verifyAll(res, label, assertions)) {
    check(res, { [result.name]: () => result.passed });
    if (!result.passed) {
      console.error(`${result.name}: ${result.error}`);
    }
  }
}

export default function () {
  for (const route of routes) {
    const res = http.get(target.baseUrl + route.path, {
      responseCallback: http.expectedStatuses(...route.expected),
      tags: { endpoint: route.label },
    });
    record(res, route.label, [[`HTTP ${route.expected[0]} contract`, route.verify]]);
    record(res, route.label, [
      ["security headers", assertSecurityHeaders],
      ["no fingerprint headers", assertNoFingerprint],
    ]);
  }
  if (__ENV.LOADTEST_MUTATION_PROOF === "1") {
    // Deliberately never incremented: the count>0 threshold must turn this run red.
    mutationProof.add(0);
  }
  sleep(Number(__ENV.SMOKE_SLEEP_SECONDS || "1"));
}