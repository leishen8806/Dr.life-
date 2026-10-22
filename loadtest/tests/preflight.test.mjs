import test from "node:test";
import assert from "node:assert/strict";

import {
  PreflightError,
  parseMetrics,
  requireEnv,
  requireMetricAtLeast,
  requireMetricFinite,
  requireMetrics,
  requireOneOf,
} from "../lib/preflight.js";

const sample = [
  "# HELP drlife_http_requests_total HTTP requests.",
  "# TYPE drlife_http_requests_total counter",
  "drlife_http_requests_total{role=\"buyer\",method=\"GET\"} 12",
  "drlife_http_requests_total{role=\"merchant\",method=\"POST\"} 3",
  "drlife_bulkhead_in_flight{pool=\"critical\"} 0",
  "drlife_bulkhead_rejections_total{pool=\"heavy\"} 7",
  "drlife_queue_depth{queue=\"critical\"} 0",
  "",
].join("\n");

test("requireEnv returns a trimmed value", () => {
  assert.equal(requireEnv({ BASE_URL: "  http://localhost:8080  " }, "BASE_URL"), "http://localhost:8080");
});

test("mutation: missing or blank env turns preflight red", () => {
  assert.throws(() => requireEnv({}, "BASE_URL"), PreflightError);
  assert.throws(() => requireEnv({ BASE_URL: "   " }, "BASE_URL"), PreflightError);
  assert.throws(() => requireEnv(undefined, "BASE_URL"), PreflightError);
});

test("requireOneOf accepts an allowed value and rejects others", () => {
  assert.equal(requireOneOf({ MODE: "local" }, "MODE", ["local", "test"]), "local");
  assert.throws(() => requireOneOf({ MODE: "prod" }, "MODE", ["local", "test"]), PreflightError);
  assert.throws(() => requireOneOf({}, "MODE", ["local", "test"]), PreflightError);
});

test("parses Prometheus exposition text and aggregates labeled samples", () => {
  const metrics = parseMetrics(sample);
  assert.equal(metrics.get("drlife_http_requests_total"), 15);
  assert.equal(metrics.get("drlife_bulkhead_in_flight"), 0);
  assert.equal(metrics.get("drlife_bulkhead_rejections_total"), 7);
  assert.equal(metrics.get("drlife_queue_depth"), 0);
});

test("requireMetrics accepts a complete snapshot", () => {
  assert.doesNotThrow(() =>
    requireMetrics(sample, [
      "drlife_http_requests_total",
      "drlife_bulkhead_in_flight",
      "drlife_bulkhead_rejections_total",
    ]),
  );
});

test("mutation: one missing metric turns preflight red and names it", () => {
  assert.throws(
    () =>
      requireMetrics(sample, [
        "drlife_http_requests_total",
        "drlife_bulkhead_in_flight",
        "drlife_brownout_level",
      ]),
    (error) => error instanceof PreflightError && /drlife_brownout_level/.test(error.message),
  );
});

test("mutation: an empty metrics document turns preflight red", () => {
  assert.throws(() => requireMetrics("", ["drlife_http_requests_total"]), PreflightError);
});

test("mutation: renaming a required metric turns preflight red", () => {
  const renamed = sample.replaceAll("drlife_bulkhead_in_flight", "drlife_bulkhead_open");
  assert.throws(() => requireMetrics(renamed, ["drlife_bulkhead_in_flight"]), PreflightError);
});

test("requireMetricFinite rejects NaN and missing values", () => {
  const withNaN = sample + "\ndrlife_broken_metric NaN\n";
  assert.doesNotThrow(() => requireMetricFinite(withNaN, "drlife_http_requests_total"));
  assert.throws(() => requireMetricFinite(withNaN, "drlife_broken_metric"), PreflightError);
  assert.throws(() => requireMetricFinite(sample, "drlife_does_not_exist"), PreflightError);
});

test("requireMetricAtLeast is a real threshold assertion", () => {
  assert.doesNotThrow(() => requireMetricAtLeast(sample, "drlife_http_requests_total", 15));
  assert.throws(() => requireMetricAtLeast(sample, "drlife_http_requests_total", 16), PreflightError);
});

test("mutation: a satisfied threshold fails when the observed value is lowered", () => {
  const lowered = sample.replace("drlife_bulkhead_rejections_total{pool=\"heavy\"} 7", "drlife_bulkhead_rejections_total{pool=\"heavy\"} 0");
  assert.doesNotThrow(() => requireMetricAtLeast(sample, "drlife_bulkhead_rejections_total", 7));
  assert.throws(() => requireMetricAtLeast(lowered, "drlife_bulkhead_rejections_total", 7), PreflightError);
});
