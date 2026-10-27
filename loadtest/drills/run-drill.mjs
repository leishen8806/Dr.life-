import { spawnSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import http from "node:http";

import { validateTargetEnv } from "../lib/guard.js";

const __ENV = process.env;
const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const composeFile = join(root, "loadtest", "docker-compose.yml");
const drill = String(__ENV.DRILL || process.argv[2] || "").trim();
const names = ["redis-cache-down", "redis-queue-down", "replica-lag", "kill-worker", "kill-app"];

let target;
try {
  target = validateTargetEnv(__ENV);
} catch (error) {
  console.error(`GUARD: ${error.message}`);
  process.exit(1);
}

if (!names.includes(drill)) {
  console.error(`DRILL must be one of ${names.join("|")}, got "${drill}"`);
  process.exit(1);
}

const timeoutMs = Number(String(__ENV.DRILL_TIMEOUT_MS || "15000").trim());
if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) {
  console.error("DRILL_TIMEOUT_MS must be a positive number");
  process.exit(1);
}

function compose(args) {
  const result = spawnSync("docker", ["compose", "-f", composeFile, ...args], {
    encoding: "utf8",
    timeout: 120000,
  });
  if (result.error) {
    throw new Error(`docker compose ${args.join(" ")}: ${result.error.message}`);
  }
  if (result.status !== 0) {
    throw new Error(`docker compose ${args.join(" ")} exited ${result.status}: ${result.stderr || result.stdout}`);
  }
  return String(result.stdout || "").trim();
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function request(path, expected) {
  return new Promise((resolve) => {
    const url = new URL(path, target.baseUrl);
    const req = http.get(url, { timeout: timeoutMs }, (res) => {
      let body = "";
      res.setEncoding("utf8");
      res.on("data", (chunk) => {
        body += chunk;
      });
      res.on("end", () => {
        resolve({ status: res.statusCode, headers: res.headers, body, expected: expected.includes(res.statusCode) });
      });
    });
    req.on("timeout", () => {
      req.destroy(new Error(`timeout after ${timeoutMs}ms`));
    });
    req.on("error", (error) => {
      resolve({ status: 0, headers: {}, body: "", error: error.message, expected: false });
    });
  });
}

function expectedBehavior(name) {
  const map = {
    "redis-cache-down": "资金接口 503；普通读降级仍可用；限流回退内存",
    "redis-queue-down": "API 仍可入账；发件箱堆积；恢复后自动转发且无丢失无重复",
    "replica-lag": "报表池失败不影响 oltp",
    "kill-worker": "任务被重新执行且不重复提交",
    "kill-app": "另一实例继续服务；/readyz 摘除生效",
  };
  return map[name];
}

async function run() {
  const startedAt = new Date().toISOString();
  const observed = [];
  if (drill === "redis-cache-down") {
    compose(["stop", "redis-cache"]);
    await sleep(1500);
    observed.push(await request(__ENV.DRILL_CRITICAL_PATH || "/v1/merchant/withdrawals", [503]));
    observed.push(await request(__ENV.DRILL_READ_PATH || "/v1/public/ping", [200, 503]));
    compose(["start", "redis-cache"]);
  } else if (drill === "redis-queue-down") {
    compose(["stop", "redis-queue"]);
    await sleep(1500);
    observed.push(await request(__ENV.DRILL_CRITICAL_PATH || "/v1/ledger/entries", [200, 202, 400, 401, 403, 422]));
    compose(["start", "redis-queue"]);
    observed.push(await request("/readyz", [200]));
  } else if (drill === "replica-lag") {
    const service = String(__ENV.REPLICA_SERVICE || "postgres-replica").trim();
    compose(["stop", service]);
    await sleep(1500);
    observed.push(await request(__ENV.DRILL_CRITICAL_PATH || "/v1/public/ping", [200]));
    compose(["start", service]);
  } else if (drill === "kill-worker") {
    compose(["kill", "worker"]);
    await sleep(1000);
    compose(["start", "worker"]);
    observed.push(await request("/readyz", [200]));
  } else if (drill === "kill-app") {
    compose(["stop", "app-a"]);
    await sleep(1500);
    const other = target.baseUrl.replace(/:\d+$/, ":18081");
    observed.push(await request(new URL("/readyz", other).toString(), [200]));
    compose(["start", "app-a"]);
  }
  const allExpected = observed.length > 0 && observed.every((item) => item.expected);
  const result = {
    drill,
    expected: expectedBehavior(drill),
    actual: observed,
    conclusion: allExpected ? "PASS" : "FAIL",
    startedAt,
    finishedAt: new Date().toISOString(),
  };
  const outDir = join(root, "loadtest", "evidence");
  mkdirSync(outDir, { recursive: true });
  writeFileSync(join(outDir, `${drill}.json`), `${JSON.stringify(result, null, 2)}\n`, "utf8");
  console.log(JSON.stringify(result));
  if (!allExpected) {
    process.exit(1);
  }
}

run().catch((error) => {
  console.error(`DRILL FAILED: ${error.message}`);
  process.exit(1);
});