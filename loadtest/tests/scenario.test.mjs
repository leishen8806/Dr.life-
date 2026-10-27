import test from "node:test";
import assert from "node:assert/strict";

import {
  ScenarioConfigError,
  assertRecovered,
  evaluateTrend,
  parseDurationSeconds,
  parsePathList,
  parsePositiveInt,
  parseRatio,
  parseRetryAfter,
  parseWeights,
  requirePath,
} from "../lib/scenario.js";

test("parseWeights accepts the default read/write/heavy mix", () => {
  assert.deepEqual(parseWeights({}), { read: 85, write: 10, heavy: 5 });
  assert.deepEqual(
    parseWeights({ MIX_READ_WEIGHT: "80", MIX_WRITE_WEIGHT: "15", MIX_HEAVY_WEIGHT: "5" }),
    { read: 80, write: 15, heavy: 5 },
  );
});

test("mutation: changing one weight without rebalancing turns the mix red", () => {
  assert.throws(
    () => parseWeights({ MIX_READ_WEIGHT: "85", MIX_WRITE_WEIGHT: "10", MIX_HEAVY_WEIGHT: "6" }),
    ScenarioConfigError,
  );
});

test("parseWeights rejects negative, fractional, blank, and non-numeric weights", () => {
  for (const env of [
    { MIX_READ_WEIGHT: "-1" },
    { MIX_WRITE_WEIGHT: "2.5" },
    { MIX_HEAVY_WEIGHT: " " },
    { MIX_READ_WEIGHT: "eighty" },
    { MIX_READ_WEIGHT: "101", MIX_WRITE_WEIGHT: "-1", MIX_HEAVY_WEIGHT: "0" },
  ]) {
    assert.throws(() => parseWeights(env), ScenarioConfigError);
  }
});

test("parseDurationSeconds accepts k6-style minute and hour durations", () => {
  assert.equal(parseDurationSeconds({}, "DURATION", "10m"), 600);
  assert.equal(parseDurationSeconds({ DURATION: "1h" }, "DURATION", "10m"), 3600);
  assert.equal(parseDurationSeconds({ DURATION: "45s" }, "DURATION", "10m"), 45);
});

test("mutation: a leaked zero or unit-less duration turns red", () => {
  for (const value of ["0s", "0m", "10", "1d", "", "  "]) {
    assert.throws(() => parseDurationSeconds({ DURATION: value }, "DURATION", "10m"), ScenarioConfigError);
  }
});

test("requirePath accepts absolute API paths", () => {
  assert.equal(requirePath("/v1/public/ping", "PING_PATH"), "/v1/public/ping");
  assert.equal(requirePath("/v1/ledger/accounts/a1/balance", "BALANCE_PATH"), "/v1/ledger/accounts/a1/balance");
});

test("mutation: relative, traversal, duplicate-slash, query, and fragment paths turn red", () => {
  for (const value of ["v1/public/ping", "/v1//ping", "/v1/../ping", "/v1/ping?x=1", "/v1/ping#x", " /v1/ping "]) {
    assert.throws(() => requirePath(value, "PING_PATH"), ScenarioConfigError);
  }
});

test("parsePathList splits and validates comma-separated paths", () => {
  assert.deepEqual(parsePathList("/v1/a, /v1/b", "READ_PATHS"), ["/v1/a", "/v1/b"]);
});

test("mutation: a blank entry in the path list turns red", () => {
  assert.throws(() => parsePathList("/v1/a,,/v1/b", "READ_PATHS"), ScenarioConfigError);
  assert.throws(() => parsePathList("", "READ_PATHS"), ScenarioConfigError);
});

test("parseRetryAfter accepts positive integer seconds", () => {
  assert.equal(parseRetryAfter("1"), 1);
  assert.equal(parseRetryAfter(" 3 "), 3);
});

test("mutation: missing, zero, negative, and non-numeric Retry-After turns red", () => {
  for (const value of [undefined, null, "", "0", "-1", "abc", "1.5"]) {
    assert.throws(() => parseRetryAfter(value), ScenarioConfigError);
  }
});

test("evaluateTrend accepts a flat or noisy series", () => {
  const flat = evaluateTrend([100, 101, 99, 100, 102, 98], { label: "goroutines", maxRatio: 1.5 });
  assert.equal(flat.leak, false);
  assert.ok(flat.ratio <= 1.5);
});

test("mutation: a steady upward series is reported as a leak", () => {
  assert.throws(
    () => evaluateTrend([100, 110, 130, 160, 200, 260], { label: "goroutines", maxRatio: 1.5 }),
    ScenarioConfigError,
  );
});

test("mutation: non-finite or too-short samples turn red instead of passing", () => {
  assert.throws(() => evaluateTrend([1, 2, Number.NaN, 3], { label: "memory" }), ScenarioConfigError);
  assert.throws(() => evaluateTrend([1, 2], { label: "memory" }), ScenarioConfigError);
  assert.throws(() => evaluateTrend([], { label: "memory" }), ScenarioConfigError);
});

test("parsePositiveInt accepts a positive integer and rejects zero, negatives, and decimals", () => {
  assert.equal(parsePositiveInt({}, "N", "2000"), 2000);
  for (const value of ["0", "-1", "1.5", "", "abc"]) {
    assert.throws(() => parsePositiveInt({ N: value }, "N", "1"), ScenarioConfigError);
  }
});

test("parseRatio accepts the documented rejection ceiling and rejects values outside 0..1", () => {
  assert.equal(parseRatio({}, "R", "0.2"), 0.2);
  for (const value of ["-0.1", "1.01", "2", "", "abc"]) {
    assert.throws(() => parseRatio({ R: value }, "R", "0.2"), ScenarioConfigError);
  }
});

test("assertRecovered accepts a return to baseline and rejects a retained peak", () => {
  assert.doesNotThrow(() => assertRecovered(10, 11, { label: "goroutines", maxRatio: 1.25 }));
  assert.throws(
    () => assertRecovered(10, 40, { label: "goroutines", maxRatio: 1.25 }),
    ScenarioConfigError,
  );
  assert.throws(() => assertRecovered(Number.NaN, 1, { label: "memory" }), ScenarioConfigError);
});
