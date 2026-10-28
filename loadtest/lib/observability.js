import http from "k6/http";

import { PreflightError, parseMetrics } from "./preflight.js";

export function scrapeMetrics(url) {
  if (typeof url !== "string" || url.trim() === "") {
    throw new PreflightError("metrics URL is required to sample recovery state");
  }
  const res = http.get(url, {
    timeout: __ENV.PREFLIGHT_TIMEOUT || "5s",
    responseCallback: http.expectedStatuses(200),
    tags: { phase: "metrics" },
  });
  if (res.status !== 200) {
    throw new PreflightError(`metrics at ${url}: unexpected HTTP ${res.status}`);
  }
  return parseMetrics(res.body);
}

export function requireMetric(metrics, name) {
  if (!metrics || typeof metrics.has !== "function") {
    throw new PreflightError("metrics sample is missing");
  }
  if (!metrics.has(name)) {
    throw new PreflightError(`missing required metric: ${name}`);
  }
  const value = metrics.get(name);
  if (!Number.isFinite(value)) {
    throw new PreflightError(`metric ${name} is not finite`);
  }
  return value;
}

export function requireMetricAtMost(metrics, name, maximum) {
  const value = requireMetric(metrics, name);
  if (!Number.isFinite(maximum)) {
    throw new PreflightError(`invalid upper bound for ${name}`);
  }
  if (value > maximum) {
    throw new PreflightError(`metric ${name} is ${value}, expected at most ${maximum}`);
  }
  return value;
}

export function gaugeOf(metrics, name) {
  return requireMetric(metrics, name);
}