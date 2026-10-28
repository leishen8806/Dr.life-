import test from "node:test";
import assert from "node:assert/strict";

import { GuardError, validateMetricsUrl, validateTargetEnv } from "../lib/guard.js";

const localEnv = (overrides = {}) => ({
  TARGET_ENV: "local",
  BASE_URL: "http://localhost:8080",
  ...overrides,
});

const accepted = [
  { name: "localhost", env: localEnv(), host: "localhost", origin: "http://localhost:8080" },
  { name: "ipv4 loopback", env: localEnv({ BASE_URL: "http://127.0.0.1:18080" }), host: "127.0.0.1", origin: "http://127.0.0.1:18080" },
  { name: "ipv6 loopback", env: localEnv({ BASE_URL: "http://[::1]:18080" }), host: "::1", origin: "http://[::1]:18080" },
  { name: "docker host bridge", env: localEnv({ BASE_URL: "http://host.docker.internal:8080" }), host: "host.docker.internal", origin: "http://host.docker.internal:8080" },
  { name: "compose app service", env: localEnv({ BASE_URL: "http://app:8080" }), host: "app", origin: "http://app:8080" },
  { name: "uppercase local env and trailing slash", env: localEnv({ TARGET_ENV: " LOCAL ", BASE_URL: "http://localhost:8080/" }), host: "localhost", origin: "http://localhost:8080" },
];

for (const item of accepted) {
  test(`accepts ${item.name}`, () => {
    const result = validateTargetEnv(item.env);
    assert.equal(result.targetEnv, "local");
    assert.equal(result.host, item.host);
    assert.equal(result.baseUrl, item.origin);
  });

  test(`mutation: ${item.name} becomes rejected when TARGET_ENV is prod`, () => {
    assert.throws(() => validateTargetEnv({ ...item.env, TARGET_ENV: "prod" }), GuardError);
  });

  test(`mutation: ${item.name} becomes rejected when the host is external`, () => {
    assert.throws(
      () => validateTargetEnv({ ...item.env, BASE_URL: "https://api.example.com" }),
      GuardError,
    );
  });
}

test("accepts an explicitly allow-listed test domain when TARGET_ENV=test", () => {
  const result = validateTargetEnv({
    TARGET_ENV: "test",
    BASE_URL: "https://loadtest.test.drlife.example",
    LOADTEST_TEST_HOSTS: "loadtest.test.drlife.example, second.test.drlife.example",
  });
  assert.equal(result.targetEnv, "test");
  assert.equal(result.host, "loadtest.test.drlife.example");
});

test("mutation: test domain is rejected without an explicit allow-list", () => {
  assert.throws(
    () => validateTargetEnv({ TARGET_ENV: "test", BASE_URL: "https://loadtest.test.drlife.example" }),
    GuardError,
  );
});

test("mutation: allow-listed test domain is still rejected when TARGET_ENV=prod", () => {
  assert.throws(
    () =>
      validateTargetEnv({
        TARGET_ENV: "prod",
        BASE_URL: "https://loadtest.test.drlife.example",
        LOADTEST_TEST_HOSTS: "loadtest.test.drlife.example",
      }),
    GuardError,
  );
});

test("mutation: compose-only app host is rejected in test env", () => {
  assert.throws(
    () =>
      validateTargetEnv({
        TARGET_ENV: "test",
        BASE_URL: "http://app:8080",
        LOADTEST_TEST_HOSTS: "app",
      }),
    GuardError,
  );
});

test("mutation: host.docker.internal is local-only", () => {
  assert.throws(
    () =>
      validateTargetEnv({
        TARGET_ENV: "test",
        BASE_URL: "http://host.docker.internal:8080",
        LOADTEST_TEST_HOSTS: "host.docker.internal",
      }),
    GuardError,
  );
});

test("accepts app-a only when explicitly allow-listed for TARGET_ENV=test", () => {
  const result = validateTargetEnv({
    TARGET_ENV: "test",
    BASE_URL: "http://app-a:8080",
    LOADTEST_TEST_HOSTS: "app-a",
  });
  assert.equal(result.host, "app-a");
});

test("mutation: app-a is rejected without its explicit test allow-list or outside TARGET_ENV=test", () => {
  assert.throws(
    () => validateTargetEnv({ TARGET_ENV: "test", BASE_URL: "http://app-a:8080" }),
    GuardError,
  );
  assert.throws(() => validateTargetEnv({ TARGET_ENV: "local", BASE_URL: "http://app-a:8080" }), GuardError);
});

test("mutation: prod-styled host remains rejected", () => {
  assert.throws(
    () =>
      validateTargetEnv({
        TARGET_ENV: "test",
        BASE_URL: "http://app-a.prod.example:8080",
        LOADTEST_TEST_HOSTS: "app-a.prod.example",
      }),
    GuardError,
  );
});

const rejected = [
  ["missing TARGET_ENV", { BASE_URL: "http://localhost:8080" }],
  ["empty TARGET_ENV", { TARGET_ENV: "  ", BASE_URL: "http://localhost:8080" }],
  ["unknown TARGET_ENV", { TARGET_ENV: "staging", BASE_URL: "http://localhost:8080" }],
  ["TARGET_ENV=prod", { TARGET_ENV: "prod", BASE_URL: "http://localhost:8080" }],
  ["TARGET_ENV=production", { TARGET_ENV: "production", BASE_URL: "http://localhost:8080" }],
  ["missing BASE_URL", { TARGET_ENV: "local" }],
  ["blank BASE_URL", { TARGET_ENV: "local", BASE_URL: " " }],
  ["malformed BASE_URL", { TARGET_ENV: "local", BASE_URL: "not a url" }],
  ["missing scheme", { TARGET_ENV: "local", BASE_URL: "localhost:8080" }],
  ["unsupported scheme", { TARGET_ENV: "local", BASE_URL: "ftp://localhost:8080" }],
  ["userinfo", { TARGET_ENV: "local", BASE_URL: "http://user:pass@localhost:8080" }],
  ["query string", { TARGET_ENV: "local", BASE_URL: "http://localhost:8080?target=prod" }],
  ["fragment", { TARGET_ENV: "local", BASE_URL: "http://localhost:8080#prod" }],
  ["base path", { TARGET_ENV: "local", BASE_URL: "http://localhost:8080/v1" }],
  ["external host", { TARGET_ENV: "local", BASE_URL: "https://api.example.com" }],
  ["lookalike loopback", { TARGET_ENV: "local", BASE_URL: "http://localhost.evil.example" }],
  ["decimal ip", { TARGET_ENV: "local", BASE_URL: "http://2130706433:8080" }],
  ["private ip", { TARGET_ENV: "local", BASE_URL: "http://10.0.0.1:8080" }],
  ["link-local ip", { TARGET_ENV: "local", BASE_URL: "http://169.254.169.254" }],
  ["port out of range", { TARGET_ENV: "local", BASE_URL: "http://localhost:70000" }],
  ["non-numeric port", { TARGET_ENV: "local", BASE_URL: "http://localhost:http" }],
  ["backslash", { TARGET_ENV: "local", BASE_URL: "http:\\\\localhost:8080" }],
];

for (const [name, env] of rejected) {
  test(`rejects ${name}`, () => {
    assert.throws(() => validateTargetEnv(env), GuardError);
  });
}

const badAllowLists = [
  ["wildcard", "*.test.drlife.example"],
  ["scheme in entry", "https://loadtest.test.drlife.example"],
  ["port in entry", "loadtest.test.drlife.example:443"],
  ["path in entry", "loadtest.test.drlife.example/v1"],
  ["empty entry", "loadtest.test.drlife.example,,second.test.drlife.example"],
  ["single label", "loadtest"],
];

for (const [name, allowList] of badAllowLists) {
  test(`rejects a bad test allow-list entry: ${name}`, () => {
    assert.throws(
      () =>
        validateTargetEnv({
          TARGET_ENV: "test",
          BASE_URL: "https://loadtest.test.drlife.example",
          LOADTEST_TEST_HOSTS: allowList,
        }),
      GuardError,
    );
  });
}

test("mutation: allow-listed production-looking host stays rejected", () => {
  assert.throws(
    () =>
      validateTargetEnv({
        TARGET_ENV: "test",
        BASE_URL: "https://api.prod.drlife.example",
        LOADTEST_TEST_HOSTS: "api.prod.drlife.example",
      }),
    GuardError,
  );
});

test("validates the metrics URL with the same host guard and allows /metrics", () => {
  const target = validateTargetEnv(localEnv());
  const metrics = validateMetricsUrl({ METRICS_URL: "http://host.docker.internal:9100/metrics" }, target);
  assert.equal(metrics.host, "host.docker.internal");
  assert.equal(metrics.url, "http://host.docker.internal:9100/metrics");
});

test("mutation: metrics URL with an external host is rejected", () => {
  const target = validateTargetEnv(localEnv());
  assert.throws(
    () => validateMetricsUrl({ METRICS_URL: "https://metrics.example.com/metrics" }, target),
    GuardError,
  );
});

test("mutation: metrics URL without /metrics is rejected", () => {
  const target = validateTargetEnv(localEnv());
  assert.throws(
    () => validateMetricsUrl({ METRICS_URL: "http://localhost:9100/healthz" }, target),
    GuardError,
  );
});
