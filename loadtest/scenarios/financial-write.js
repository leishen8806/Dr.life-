import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

import { assertStatusIn, assertNoDeadlock, verifyAll } from "../lib/checks.js";
import { validateTargetEnv } from "../lib/guard.js";
import { requireBearerToken, requirePostRoute, requireRoute } from "../lib/k6-preflight.js";
import { parsePositiveInt, requirePath } from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const withdrawalPath = requirePath(__ENV.WITHDRAWAL_PATH || "/v1/merchant/withdrawals", "WITHDRAWAL_PATH");
const probePath = requirePath(__ENV.WITHDRAWAL_PROBE_PATH || "/v1/merchant/withdrawals/probe", "WITHDRAWAL_PROBE_PATH");
const vus = parsePositiveInt(__ENV, "FIN_VUS", "40");
const duration = String(__ENV.FIN_DURATION || "60s").trim();
const amount = String(__ENV.FIN_AMOUNT || "1000").trim();
const body = String(__ENV.FIN_BODY || JSON.stringify({ amount: Number(amount), currency: "USD" })).trim();
if (body === "") {
  throw new Error("FIN_BODY must not be blank");
}

const criticalDuration = new Trend("critical_duration", true);
const negativeBalance = new Counter("negative_balance");
const idempotencyMismatch = new Counter("idempotency_mismatch");

export const options = {
  vus,
  duration,
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    critical_duration: ["p(95)<1000"],
    negative_balance: ["count==0"],
    idempotency_mismatch: ["count==0"],
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

function bodyText(res) {
  return typeof res.body === "string" ? res.body : "";
}

function extractId(res) {
  const text = bodyText(res);
  if (text === "") {
    return "";
  }
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed === "object") {
      if (typeof parsed.id === "string") {
        return parsed.id;
      }
      if (parsed.data && typeof parsed.data === "object" && typeof parsed.data.id === "string") {
        return parsed.data.id;
      }
    }
  } catch (error) {
    return "";
  }
  return "";
}

export function setup() {
  const token = requireBearerToken(__ENV);
  const headers = { Authorization: `Bearer ${token}`, "Content-Type": "application/json" };
  requireRoute(target.baseUrl + withdrawalPath, "withdrawal read", [200, 401, 403], null, headers);
  requirePostRoute(
    target.baseUrl + probePath,
    body,
    "withdrawal probe",
    [400, 401, 403, 404, 409, 422],
    { ...headers, "Idempotency-Key": `fin-preflight-${Date.now()}` },
    assertStatusIn,
  );
  return { token };
}

export function financialWrite(data) {
  const key = `fin-${__VU}-${__ITER}-${Date.now()}`;
  const headers = {
    Authorization: `Bearer ${data.token}`,
    "Content-Type": "application/json",
    "Idempotency-Key": key,
  };
  const first = http.post(target.baseUrl + withdrawalPath, body, {
    headers,
    responseCallback: http.expectedStatuses(200, 201, 202, 400, 401, 403, 409, 422),
    tags: { kind: "critical" },
  });
  criticalDuration.add(first.timings.duration);
  record(first, "financial write", [
    ["financial status contract", (res) => assertStatusIn(res, [200, 201, 202, 400, 401, 403, 409, 422], "financial write")],
    ["no deadlock", assertNoDeadlock],
  ]);
  if (/"(?:balance|available_balance)"\s*:\s*-/.test(bodyText(first))) {
    negativeBalance.add(1);
  }

  const second = http.post(target.baseUrl + withdrawalPath, body, {
    headers,
    responseCallback: http.expectedStatuses(200, 201, 202, 400, 401, 403, 409, 422),
    tags: { kind: "critical" },
  });
  criticalDuration.add(second.timings.duration);
  const firstId = extractId(first);
  const secondId = extractId(second);
  if (firstId !== "" && secondId !== "" && firstId !== secondId) {
    idempotencyMismatch.add(1);
  }
  if (first.status >= 200 && first.status < 300 && second.status >= 200 && second.status < 300 && firstId === "" && secondId === "") {
    // A successful repeated command must expose an id; otherwise replay safety is unverifiable.
    idempotencyMismatch.add(1);
  }
  record(second, "financial replay", [
    ["replay status contract", (res) => assertStatusIn(res, [200, 201, 202, 400, 401, 403, 409, 422], "financial replay")],
  ]);
  sleep(Number(__ENV.FIN_SLEEP_SECONDS || "0.05"));
}