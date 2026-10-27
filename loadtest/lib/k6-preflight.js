import http from "k6/http";

import { PreflightError, requireMetrics } from "./preflight.js";

function describe(res) {
  if (res.status === 0) {
    return `target is unreachable (${res.error || "no response"})`;
  }
  return `unexpected HTTP ${res.status}`;
}

export function requireRoute(url, label, expectedStatuses, verify, headers) {
  if (!Array.isArray(expectedStatuses) || expectedStatuses.length === 0) {
    throw new PreflightError(`${label}: expectedStatuses must not be empty`);
  }
  const options = {
    timeout: __ENV.PREFLIGHT_TIMEOUT || "5s",
    responseCallback: http.expectedStatuses(...expectedStatuses),
    tags: { phase: "preflight", endpoint: label },
  };
  if (headers && typeof headers === "object") {
    options.headers = headers;
  }
  const res = http.get(url, options);
  if (!expectedStatuses.includes(res.status)) {
    throw new PreflightError(`${label} at ${url}: ${describe(res)}`);
  }
  if (verify) {
    try {
      verify(res);
    } catch (error) {
      throw new PreflightError(`${label} at ${url}: ${error.message}`);
    }
  }
  return res;
}

export function requirePostRoute(url, body, label, expectedStatuses, headers, verify) {
  if (!Array.isArray(expectedStatuses) || expectedStatuses.length === 0) {
    throw new PreflightError(`${label}: expectedStatuses must not be empty`);
  }
  const requestHeaders = { "Content-Type": "application/json" };
  if (headers && typeof headers === "object") {
    for (const [name, value] of Object.entries(headers)) {
      requestHeaders[name] = value;
    }
  }
  const res = http.post(url, body, {
    headers: requestHeaders,
    timeout: __ENV.PREFLIGHT_TIMEOUT || "5s",
    responseCallback: http.expectedStatuses(...expectedStatuses),
    tags: { phase: "preflight", endpoint: label },
  });
  if (!expectedStatuses.includes(res.status)) {
    throw new PreflightError(`${label} at ${url}: ${describe(res)}`);
  }
  if (verify) {
    try {
      verify(res);
    } catch (error) {
      throw new PreflightError(`${label} at ${url}: ${error.message}`);
    }
  }
  return res;
}

export function requireMetricsEndpoint(url, requiredNames) {
  const res = http.get(url, {
    timeout: __ENV.PREFLIGHT_TIMEOUT || "5s",
    responseCallback: http.expectedStatuses(200),
    tags: { phase: "preflight", endpoint: "metrics" },
  });
  if (res.status !== 200) {
    throw new PreflightError(`metrics at ${url}: ${describe(res)}`);
  }
  try {
    return requireMetrics(res.body, requiredNames);
  } catch (error) {
    throw new PreflightError(`metrics at ${url}: ${error.message}`);
  }
}

export function requireBearerToken(env) {
  const raw = String(env.LOADTEST_BEARER_TOKEN || "").trim();
  if (raw === "") {
    throw new PreflightError(
      "LOADTEST_BEARER_TOKEN is required for authenticated scenarios; use the local test issuer, never a real token",
    );
  }
  if (!/^[A-Za-z0-9._~+/=-]+$/.test(raw)) {
    throw new PreflightError("LOADTEST_BEARER_TOKEN must be a single bearer token without whitespace");
  }
  return raw;
}

export function requirePositiveInt(env, name, fallback) {
  const raw = String(env[name] === undefined || env[name] === null ? fallback : env[name]).trim();
  const value = Number(raw);
  if (!Number.isInteger(value) || value < 1) {
    throw new PreflightError(`${name} must be a positive integer, got "${raw}"`);
  }
  return value;
}
