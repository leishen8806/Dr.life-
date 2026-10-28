import http from "k6/http";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";

import { assertStatusIn, verifyAll } from "../lib/checks.js";
import { validateTargetEnv } from "../lib/guard.js";
import { requireBearerToken, requirePostRoute, requireRoute } from "../lib/k6-preflight.js";
import { parsePositiveInt, requirePath } from "../lib/scenario.js";

const target = validateTargetEnv(__ENV);
const unauthorizedPath = requirePath(__ENV.SECURITY_UNAUTHORIZED_PATH || "/v1/buyer/loadtest-probe", "SECURITY_UNAUTHORIZED_PATH");
const idempotencyPath = requirePath(__ENV.SECURITY_IDEMPOTENCY_PATH || "", "SECURITY_IDEMPOTENCY_PATH");
const rateLimitPath = requirePath(__ENV.RATE_LIMIT_PROBE_PATH || "/v1/public/ping", "RATE_LIMIT_PROBE_PATH");
const vus = parsePositiveInt(__ENV, "SECURITY_VUS", "4");
const probes = parsePositiveInt(__ENV, "RATE_LIMIT_PROBES", "120");
const duration = String(__ENV.SECURITY_DURATION || "30s").trim();
if (probes < 2) {
  throw new Error("RATE_LIMIT_PROBES must be at least 2");
}

const acceptedWrite = [200, 201];
const replayStatuses = [200, 201, 409];

const unauthorizedAllowed = new Counter("unauthorized_allowed");
const idempotencyDuplicateExecution = new Counter("idempotency_duplicate_execution");
const idempotencyContractMissing = new Counter("idempotency_contract_missing");
const rateLimitMissing = new Counter("rate_limit_missing");

export const options = {
  vus,
  duration,
  thresholds: {
    checks: ["rate>0.99"],
    unauthorized_allowed: ["count==0"],
    idempotency_duplicate_execution: ["count==0"],
    idempotency_contract_missing: ["count==0"],
    rate_limit_missing: ["count==0"],
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

function identityOf(res, label) {
  let body;
  try {
    body = JSON.parse(res.body);
  } catch (error) {
    throw new Error(`${label}: response body is not valid JSON (${error.message})`);
  }
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw new Error(`${label}: response body must be a JSON object`);
  }
  const candidates = [body.id, body.entry_id, body.ledger_entry_id, body.data && body.data.id];
  for (const value of candidates) {
    if ((typeof value === "string" && value.trim() !== "") || Number.isInteger(value)) {
      return String(value);
    }
  }
  throw new Error(`${label}: response must expose id, entry_id, ledger_entry_id, or data.id`);
}

export function setup() {
  requireRoute(target.baseUrl + unauthorizedPath, "unauthorized probe", [401, 403, 404], function (res) {
    assertStatusIn(res, [401, 403, 404], "unauthorized probe");
  });
  const token = requireBearerToken(__ENV);
  const headers = {
    Authorization: `Bearer ${token}`,
    "Idempotency-Key": `security-preflight-${Date.now()}`,
  };
  requirePostRoute(
    target.baseUrl + idempotencyPath,
    '{"probe":true}',
    "idempotency probe",
    acceptedWrite,
    headers,
    function (res) {
      identityOf(res, "idempotency probe");
    },
  );
  return { startedAt: new Date().toISOString(), token };
}

export default function (data) {
  const denied = http.get(target.baseUrl + unauthorizedPath, {
    responseCallback: http.expectedStatuses(401, 403, 404),
    tags: { kind: "unauthorized" },
  });
  if (![401, 403, 404].includes(denied.status)) {
    unauthorizedAllowed.add(1);
  }
  record(denied, "unauthorized", [["denied contract", (res) => assertStatusIn(res, [401, 403, 404], "unauthorized")]]);

  const key = `security-${__VU}-${__ITER}-${Date.now()}`;
  const headers = { Authorization: `Bearer ${data.token}`, "Content-Type": "application/json", "Idempotency-Key": key };
  const first = http.post(target.baseUrl + idempotencyPath, '{"probe":true}', {
    headers,
    responseCallback: http.expectedStatuses(...replayStatuses),
    tags: { kind: "idempotency" },
  });
  record(first, "idempotency first", [["first write accepted", (res) => assertStatusIn(res, acceptedWrite, "idempotency first")]]);
  const second = http.post(target.baseUrl + idempotencyPath, '{"probe":true}', {
    headers,
    responseCallback: http.expectedStatuses(...replayStatuses),
    tags: { kind: "idempotency" },
  });
  const firstAccepted = acceptedWrite.includes(first.status);
  const secondAccepted = acceptedWrite.includes(second.status);
  if (!firstAccepted) {
    idempotencyContractMissing.add(1);
    check(second, { "idempotency probe wrote once": () => false });
  } else if (second.status === 409) {
    check(second, { "replay rejected without re-execution": () => true });
  } else if (!secondAccepted) {
    idempotencyContractMissing.add(1);
    check(second, { "replay returned a write or 409": () => false });
  } else {
    let firstId = "";
    let secondId = "";
    try {
      firstId = identityOf(first, "idempotency first");
      secondId = identityOf(second, "idempotency replay");
    } catch (error) {
      idempotencyContractMissing.add(1);
      check(second, { "idempotency responses expose an identity": () => false });
      console.error(error.message);
    }
    if (firstId !== "" && secondId !== "") {
      check(second, { "replay returns the same identity": () => firstId === secondId });
      if (firstId !== secondId) {
        idempotencyDuplicateExecution.add(1);
      }
    }
  }

  let sawLimit = false;
  for (let index = 0; index < probes; index += 1) {
    const res = http.get(target.baseUrl + rateLimitPath, {
      responseCallback: http.expectedStatuses(200, 429),
      tags: { kind: "rate-limit" },
    });
    if (res.status === 429) {
      sawLimit = true;
      if (String(res.headers["Retry-After"] || "").trim() === "") {
        rateLimitMissing.add(1);
      }
      break;
    }
  }
  if (!sawLimit) {
    rateLimitMissing.add(1);
  }
  sleep(Number(__ENV.SECURITY_SLEEP_SECONDS || "0.05"));
}