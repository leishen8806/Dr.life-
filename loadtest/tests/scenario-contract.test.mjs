import test from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (relative) => readFileSync(join(root, relative), "utf8");

const requiredThresholds = {
  "scenarios/smoke.js": ["checks", "http_req_failed"],
  "scenarios/login-storm.js": ["http_req_failed", "login_success_duration", "critical_ok", "health_ok", "login_retry_after_missing"],
  "scenarios/mixed.js": ["read_duration", "write_duration", "http_req_failed", "writes_deadlocked"],
  "scenarios/financial-write.js": ["critical_duration", "http_req_failed", "negative_balance", "idempotency_mismatch"],
  "scenarios/overload3x.js": ["rejection_duration", "critical_ok", "health_ok", "heavy_rejections", "heavy_retry_after_missing"],
  "scenarios/retry-storm.js": ["retry_immediate_elapsed", "retry_backoff_elapsed", "http_req_failed"],
  "scenarios/cache-stampede.js": ["cache_origin_delta", "checks"],
  "scenarios/queue-flood.js": ["critical_duration", "outbox_lag_seconds", "http_req_failed"],
  "scenarios/soak.js": ["checks", "soak_leak_detected"],
  "scenarios/security-under-load.js": ["unauthorized_allowed", "idempotency_duplicate_execution", "rate_limit_missing", "checks"],
};

test("every required scenario exists", () => {
  for (const relative of Object.keys(requiredThresholds)) {
    assert.equal(existsSync(join(root, "loadtest", relative)), true, `missing loadtest/${relative}`);
  }
});

test("every scenario validates the target at module load and never uses optional chaining", () => {
  for (const relative of Object.keys(requiredThresholds)) {
    const source = read(`loadtest/${relative}`);
    assert.match(source, /const\s+target\s*=\s*validateTargetEnv\(__ENV\)/, `${relative} must call validateTargetEnv(__ENV) at module load`);
    assert.doesNotMatch(source, /\?\./, `${relative} must not use optional chaining to skip a missing target`);
    assert.match(source, /from\s+"\.\.\/lib\/guard\.js"/, `${relative} must import the environment guard`);
  }
});

test("every scenario fails closed before load instead of skipping a missing target", () => {
  for (const relative of Object.keys(requiredThresholds)) {
    const source = read(`loadtest/${relative}`);
    assert.match(source, /export function setup\(/, `${relative} must preflight in setup()`);
    assert.match(source, /throw new (?:PreflightError|ScenarioConfigError|GuardError|Error)/, `${relative} must throw when a required target is missing`);
    assert.doesNotMatch(source, /if\s*\(\s*!\s*[A-Za-z_$][\w$]*\s*\)\s*\{?\s*return\b/, `${relative} must not silently return when a required value is missing`);
    assert.doesNotMatch(source, /\.skip\s*\(/, `${relative} must not use test.skip()`);
  }
});

test("every scenario declares its threshold contract", () => {
  for (const [relative, names] of Object.entries(requiredThresholds)) {
    const source = read(`loadtest/${relative}`);
    assert.match(source, /thresholds\s*[:=]/, `${relative} must declare thresholds`);
    for (const name of names) {
      assert.match(source, new RegExp(`\\b${name}\\b`), `${relative} is missing threshold metric ${name}`);
    }
  }
});

test("README declares the production prohibition in the first line and both shells", () => {
  const readme = read("loadtest/README.md");
  assert.equal(readme.split(/\r?\n/, 1)[0], "禁止对生产运行");
  assert.match(readme, /PowerShell/);
  assert.match(readme, /bash/);
  assert.match(readme, /grafana\/k6:2\.3\.0/);
});

test("the load-test workflow is manual only and can never become a PR gate", () => {
  const workflow = read(".github/workflows/loadtest.yml");
  assert.match(workflow, /^\s*workflow_dispatch:\s*$/m);
  assert.doesNotMatch(workflow, /^\s*pull_request:\s*$/m);
  assert.doesNotMatch(workflow, /^\s*push:\s*$/m);
  assert.match(workflow, /grafana\/k6:2\.3\.0/);
});

test("compose pins every image and includes the required local services", () => {
  const compose = read("loadtest/docker-compose.yml");
  for (const fragment of [
    "postgres:16-alpine",
    "redis:7-alpine",
    "drlife-loadtest:base",
    "prom/prometheus:v3.7.3",
    "grafana/grafana:12.3.0",
    "redis-cache:",
    "redis-queue:",
    "prometheus:",
    "grafana:",
    "worker:",
    "app-a:",
    "app-b:",
  ]) {
    assert.match(compose, new RegExp(fragment.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), `compose is missing ${fragment}`);
  }
  assert.doesNotMatch(compose, /image:\s*[^\s]+:latest\b/, "compose must not use latest tags");
});

test("worker overrides the HTTP image health check with a process probe", () => {
  const compose = read("loadtest/docker-compose.yml");
  const worker = compose.match(/^  worker:\n([\s\S]*?)(?=^  \w|\z)/m)?.[1] ?? "";

  assert.match(worker, /healthcheck:\n\s+test: \["CMD-SHELL", "kill -0 1"\]/);
  assert.doesNotMatch(worker, /healthz/);
});

test("the rig applies goose migrations before app and worker start", () => {
  const compose = read("loadtest/docker-compose.yml");
  const migrate = compose.match(/^  migrate:\n([\s\S]*?)(?=^  \w|\z)/m)?.[1] ?? "";
  for (const fragment of [
    'entrypoint: ["/migrate"]',
    "postgres:",
    "condition: service_healthy",
  ]) {
    assert.match(migrate, new RegExp(fragment.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), `migration service is missing ${fragment}`);
  }
  for (const service of ["app-a", "app-b", "worker"]) {
    const body = compose.match(new RegExp(`^  ${service}:\\n([\\s\\S]*?)(?=^  \\w|\\z)`, "m"))?.[1] ?? "";
    assert.match(body, /migrate:\n\s+condition: service_completed_successfully/, `${service} must wait for migrations`);
  }
});

test("the five fault drills are present and guarded", () => {
  const drills = read("loadtest/drills/run-drill.mjs");
  for (const name of ["redis-cache-down", "redis-queue-down", "replica-lag", "kill-worker", "kill-app"]) {
    assert.match(drills, new RegExp(`["']${name}["']`), `drill runner is missing ${name}`);
  }
  assert.match(drills, /validateTargetEnv\(__ENV\)/);
  assert.match(drills, /process\.exitCode\s*=\s*1|process\.exit\(1\)/);
});

test("the slowloris probe is guarded and refuses production", () => {
  const probe = read("loadtest/tools/slowloris.mjs");
  assert.match(probe, /validateTargetEnv/);
  assert.match(probe, /process\.exitCode\s*=\s*1|process\.exit\(1\)/);
  assert.doesNotMatch(probe, /\?\./);
});
const exactThresholds = {
  "scenarios/smoke.js": [
    ["checks", "rate>0.99"],
    ["http_req_failed", "rate<0.01"],
    ["http_req_duration", "p(95)<500"],
    ["mutation_proof", "count>0"],
  ],
  "scenarios/login-storm.js": [
    ["http_req_failed", "rate<0.01"],
    ["login_success_duration", "p(95)<1000"],
    ["login_rejection_duration", "p(95)<200"],
    ["login_retry_after_missing", "count==0"],
    ["login_unexpected", "count==0"],
    ["critical_ok", "rate>0.99"],
    ["health_ok", "rate>0.99"],
  ],
  "scenarios/mixed.js": [
    ["http_req_failed", "rate<0.005"],
    ["read_duration", "p(95)<500"],
    ["write_duration", "p(95)<1000"],
    ["writes_deadlocked", "count==0"],
  ],
  "scenarios/financial-write.js": [
    ["http_req_failed", "rate<0.01"],
    ["critical_duration", "p(95)<1000"],
    ["negative_balance", "count==0"],
    ["idempotency_mismatch", "count==0"],
  ],
  "scenarios/overload3x.js": [
    ["http_req_failed", "rate<0.01"],
    ["rejection_duration", "p(95)<200"],
    ["critical_ok", "rate>0.99"],
    ["health_ok", "rate>0.99"],
    ["heavy_rejections", "count>0"],
    ["heavy_retry_after_missing", "count==0"],
  ],
  "scenarios/retry-storm.js": [
    ["http_req_failed", "rate<0.01"],
    ["retry_immediate_elapsed", "p(95)<5000"],
    ["retry_backoff_elapsed", "p(95)<5000"],
  ],
  "scenarios/cache-stampede.js": [
    ["checks", "rate>0.99"],
    ["cache_origin_delta", "p(95)<=1"],
  ],
  "scenarios/queue-flood.js": [
    ["http_req_failed", "rate<0.01"],
    ["critical_duration", "p(95)<1000"],
    ["outbox_lag_seconds", "value<=60"],
  ],
  "scenarios/soak.js": [
    ["checks", "rate>0.99"],
    ["http_req_failed", "rate<0.01"],
    ["soak_leak_detected", "count==0"],
  ],
  "scenarios/security-under-load.js": [
    ["checks", "rate>0.99"],
    ["unauthorized_allowed", "count==0"],
    ["idempotency_duplicate_execution", "count==0"],
    ["idempotency_contract_missing", "count==0"],
    ["rate_limit_missing", "count==0"],
  ],
};

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

test("threshold bounds are pinned to the acceptance numbers", () => {
  for (const [relative, entries] of Object.entries(exactThresholds)) {
    const source = read(`loadtest/${relative}`).replace(/\s+/g, " ");
    for (const [metric, bound] of entries) {
      const pattern = new RegExp(
        `(?:^|[ {,.])["']?${escapeRegExp(metric)}["']?\\s*(?::|=)\\s*\\[\\s*["']${escapeRegExp(bound)}["']`,
      );
      assert.match(source, pattern, `${relative}: ${metric} must be pinned to ${bound}`);
    }
  }
});

test("overload defaults match the compose bulkhead route classes", () => {
  const overload = read("loadtest/scenarios/overload3x.js");
  const compose = read("loadtest/docker-compose.yml");
  assert.match(overload, /OVERLOAD_HEAVY_PATH \|\| "\/v1\/public\/ping"/);
  assert.match(overload, /OVERLOAD_CRITICAL_PATH \|\| "\/readyz"/);
  assert.match(overload, /HEALTH_PATH \|\| "\/healthz"/);
  assert.match(compose, /BULKHEAD_ROUTE_CLASSES: "\/v1\/public\/ping=heavy,\/readyz=critical"/);
});

test("the security idempotency target must be explicit and a 404 cannot satisfy it", () => {
  const source = read("loadtest/scenarios/security-under-load.js");
  assert.match(source, /SECURITY_IDEMPOTENCY_PATH \|\| ""/);
  assert.doesNotMatch(source, /const acceptedWrite = \[[^\]]*404/);
  assert.doesNotMatch(source, /"idempotency probe",\s*\[[^\]]*404/);
  assert.doesNotMatch(source, /const replayStatuses = \[[^\]]*404/);
});

test("the replica drill has a real service in the compose rig", () => {
  const compose = read("loadtest/docker-compose.yml");
  assert.match(compose, /^  postgres-replica:$/m);
  assert.match(compose, /REPORT_DATABASE_URL: "postgres:\/\/drlife:loadtest-only@postgres-replica:5432\/drlife\?sslmode=disable"/);
});
test("the CI smoke workflow is path-filtered and proves the guard every run", () => {
  const workflow = read(".github/workflows/loadtest-smoke.yml");
  assert.match(workflow, /^\s*pull_request:\s*$/m);
  assert.match(workflow, /paths:\s*\n\s*- "loadtest\/\*\*"/);
  assert.match(workflow, /grafana\/k6:2\.3\.0/);
  assert.match(workflow, /-e TARGET_ENV=prod\b/);
  assert.match(workflow, /if \[ "\$code" -eq 0 \]/);
  assert.match(workflow, /TARGET_ENV: test/);
  assert.match(workflow, /LOADTEST_TEST_HOSTS: app-a/);
  assert.match(workflow, /-e TARGET_ENV -e BASE_URL -e LOADTEST_TEST_HOSTS/);
  assert.match(workflow, /loadtest\/scenarios\/smoke\.js/);
});
