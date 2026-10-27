import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

import {
  assert2xx,
  assertNoDeadlock,
  assertStatusIn,
  verifyAll,
} from "../lib/checks.js";
import { validateTargetEnv } from "../lib/guard.js";
import { requireBearerToken, requirePostRoute, requireRoute } from "../lib/k6-preflight.js";
import {
  parseDurationSeconds,
  parsePositiveInt,
  parseWeights,
  requirePath,
} from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const weights = parseWeights(__ENV);
const durationSeconds = parseDurationSeconds(__ENV, "MIX_DURATION", "10m");
const vus = parsePositiveInt(__ENV, "MIX_VUS", "20");
const readPath = requirePath(__ENV.MIX_READ_PATH || "/v1/buyer/orders", "MIX_READ_PATH");
const writePath = requirePath(__ENV.MIX_WRITE_PATH || "/v1/buyer/orders", "MIX_WRITE_PATH");
const heavyPath = requirePath(__ENV.MIX_HEAVY_PATH || "/v1/buyer/reports/summary", "MIX_HEAVY_PATH");
const writeBody = String(__ENV.MIX_WRITE_BODY || '{"items":[],"note":"loadtest"}').trim();
if (writeBody === "") {
  throw new Error("MIX_WRITE_BODY must not be blank");
}

const readDuration = new Trend("read_duration", true);
const writeDuration = new Trend("write_duration", true);
const heavyDuration = new Trend("heavy_duration", true);
const writesDeadlocked = new Counter("writes_deadlocked");
const writeAccepted = new Counter("write_accepted");

export const options = {
  scenarios: {
    mixed: {
      executor: "constant-vus",
      vus,
      duration: `${durationSeconds}s`,
      exec: "mixed",
    },
  },
  thresholds: {
    checks: ["rate>0.99"],
    "http_req_failed": ["rate<0.005"],
    "http_req_duration{kind:read}": ["p(95)<500"],
    "http_req_duration{kind:write}": ["p(95)<1000"],
    read_duration: ["p(95)<500"],
    write_duration: ["p(95)<1000"],
    writes_deadlocked: ["count==0"],
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
  const headers = { Authorization: `Bearer ${token}` };
  requireRoute(target.baseUrl + readPath, "mixed read", [200], assert2xx, headers);
  requireRoute(target.baseUrl + heavyPath, "mixed heavy", [200], assert2xx, headers);
  requirePostRoute(
    target.baseUrl + writePath,
    writeBody,
    "mixed write probe",
    [400, 422],
    { ...headers, "Content-Type": "application/json", "Idempotency-Key": `preflight-${Date.now()}` },
    assertStatusIn,
  );
  return { startedAt: new Date().toISOString(), token };
}

export function mixed(data) {
  const headers = {
    Authorization: `Bearer ${data.token}`,
    "Content-Type": "application/json",
  };
  const slot = (__ITER * 7 + __VU * 13) % 100;
  if (slot < weights.read) {
    const res = http.get(target.baseUrl + readPath, {
      headers: { Authorization: headers.Authorization },
      responseCallback: http.expectedStatuses(200),
      tags: { kind: "read" },
    });
    readDuration.add(res.timings.duration);
    record(res, "mixed read", [["read accepted", assert2xx]]);
  } else if (slot < weights.read + weights.write) {
    const res = http.post(target.baseUrl + writePath, writeBody, {
      headers: { ...headers, "Idempotency-Key": `mix-${__VU}-${__ITER}-${Date.now()}` },
      responseCallback: http.expectedStatuses(200, 201, 202, 409, 422),
      tags: { kind: "write" },
    });
    writeDuration.add(res.timings.duration);
    if (res.status >= 200 && res.status < 300) {
      writeAccepted.add(1);
    }
    record(res, "mixed write", [
      ["write status contract", (r) => assertStatusIn(r, [200, 201, 202, 409, 422], "mixed write")],
      ["write has no deadlock", assertNoDeadlock],
    ]);
  } else {
    const res = http.get(target.baseUrl + heavyPath, {
      headers: { Authorization: headers.Authorization },
      responseCallback: http.expectedStatuses(200),
      tags: { kind: "heavy" },
    });
    heavyDuration.add(res.timings.duration);
    record(res, "mixed heavy", [["heavy accepted", assert2xx]]);
  }
  sleep(Number(__ENV.MIX_SLEEP_SECONDS || "0.2"));
}
