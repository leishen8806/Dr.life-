import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

import { assert2xx, assertHealthz, assertRetryAfter, verifyAll } from "../lib/checks.js";
import { validateTargetEnv } from "../lib/guard.js";
import { unwrapList } from "../lib/k6-common.js";
import { requirePostRoute, requireRoute } from "../lib/k6-preflight.js";
import { PreflightError } from "../lib/preflight.js";
import {
  parseDurationSeconds,
  parsePositiveInt,
  parseRatio,
  requirePath,
} from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const loginPath = requirePath(__ENV.LOGIN_PATH || "/v1/auth/telegram", "LOGIN_PATH");
const criticalPath = requirePath(__ENV.LOADTEST_CRITICAL_PATH || "/v1/public/ping", "LOADTEST_CRITICAL_PATH");
const count = parsePositiveInt(__ENV, "LOGIN_COUNT", "2000");
const durationSeconds = parseDurationSeconds(__ENV, "LOGIN_DURATION", "60s");
const rejectionMax = parseRatio(__ENV, "LOGIN_REJECTION_MAX", "0.2");
const preAllocatedVUs = parsePositiveInt(__ENV, "LOGIN_PRE_VUS", "200");
const maxVUs = parsePositiveInt(__ENV, "LOGIN_MAX_VUS", "1000");
const initDataPath = String(__ENV.LOADTEST_INITDATA_FILE || "").trim();

if (initDataPath === "") {
  throw new PreflightError("LOADTEST_INITDATA_FILE is required; never use a real Telegram initData");
}
const initData = unwrapList(open(initDataPath), "LOADTEST_INITDATA_FILE");
if (initData.length === 0) {
  throw new PreflightError("LOADTEST_INITDATA_FILE must contain at least one synthetic initData value");
}

const loginSuccessDuration = new Trend("login_success_duration", true);
const loginRejectionDuration = new Trend("login_rejection_duration", true);
const loginRejections = new Counter("login_rejections");
const loginRetryAfterMissing = new Counter("login_retry_after_missing");
const loginUnexpected = new Counter("login_unexpected");
const loginRejectionRate = new Rate("login_rejection_rate");
const criticalOk = new Rate("critical_ok");
const healthOk = new Rate("health_ok");

export const options = {
  scenarios: {
    storm: {
      executor: "constant-arrival-rate",
      rate: count,
      timeUnit: "60s",
      duration: `${durationSeconds}s`,
      preAllocatedVUs,
      maxVUs,
      exec: "loginStorm",
    },
  },
  thresholds: {
    checks: ["rate>0.99"],
    http_req_failed: ["rate<0.01"],
    login_success_duration: ["p(95)<1000"],
    login_rejection_duration: ["p(95)<200"],
    login_rejection_rate: [`rate<=${rejectionMax}`],
    login_retry_after_missing: ["count==0"],
    login_unexpected: ["count==0"],
    critical_ok: ["rate>0.99"],
    health_ok: ["rate>0.99"],
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
  requirePostRoute(
    target.baseUrl + loginPath,
    JSON.stringify({ init_data: initData[0] }),
    "login",
    [200, 201],
    { "Content-Type": "application/json" },
    assert2xx,
  );
  requireRoute(target.baseUrl + "/healthz", "healthz", [200], assertHealthz);
  requireRoute(target.baseUrl + criticalPath, "critical", [200], assert2xx);
  return { startedAt: new Date().toISOString(), count };
}

export function loginStorm() {
  const body = JSON.stringify({ init_data: initData[__ITER % initData.length] });
  const res = http.post(target.baseUrl + loginPath, body, {
    headers: { "Content-Type": "application/json" },
    responseCallback: http.expectedStatuses(200, 201, 429),
    tags: { kind: "login" },
  });

  loginRejectionRate.add(res.status === 429);
  if (res.status === 429) {
    loginRejections.add(1);
    loginRejectionDuration.add(res.timings.duration);
    let valid = true;
    try {
      assertRetryAfter(res, "login rejection", 1, 3);
    } catch (error) {
      valid = false;
      console.error(error.message);
    }
    if (!valid) {
      loginRetryAfterMissing.add(1);
    }
    check(res, { "login rejection carries Retry-After 1-3s": () => valid });
  } else if (res.status === 200 || res.status === 201) {
    loginSuccessDuration.add(res.timings.duration);
    record(res, "login", [["login accepted", assert2xx]]);
  } else {
    loginUnexpected.add(1);
    record(res, "login unexpected status", [["login accepted", assert2xx]]);
  }

  const health = http.get(target.baseUrl + "/healthz", {
    responseCallback: http.expectedStatuses(200),
    tags: { kind: "health" },
  });
  const healthPassed = health.status === 200;
  healthOk.add(healthPassed);
  record(health, "healthz", [["health contract", assertHealthz]]);

  const critical = http.get(target.baseUrl + criticalPath, {
    responseCallback: http.expectedStatuses(200),
    tags: { kind: "critical" },
  });
  const criticalPassed = critical.status === 200;
  criticalOk.add(criticalPassed);
  record(critical, "critical", [["critical contract", assert2xx]]);

  sleep(Number(__ENV.LOGIN_SLEEP_SECONDS || "0"));
}