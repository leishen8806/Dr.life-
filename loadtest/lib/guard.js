const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "::1"]);
const LOCAL_ONLY_HOSTS = new Set(["host.docker.internal", "app"]);
const COMPOSE_TEST_HOSTS = new Set(["app-a"]);
const FORBIDDEN_HOST_LABELS = new Set(["prod", "production", "live"]);
const IPV4_RE = /^(?:\d{1,3}\.){3}\d{1,3}$/;
const HOSTNAME_RE = /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/;

export class GuardError extends Error {
  constructor(message) {
    super(message);
    this.name = "GuardError";
  }
}

function fail(message) {
  throw new GuardError(message);
}

function parseStrictUrl(raw, label, allowedPath) {
  const value = String(raw === undefined || raw === null ? "" : raw).trim();
  if (value === "") {
    fail(`${label} is required`);
  }
  if (/[\s\\]/.test(value)) {
    fail(`${label} must not contain whitespace or backslashes`);
  }

  const match = /^(https?):\/\/([^/?#]*)([^?#]*)$/i.exec(value);
  if (!match) {
    fail(`${label} must be an absolute http(s) URL without a query or fragment`);
  }

  const scheme = match[1].toLowerCase();
  const authority = match[2];
  let path = match[3] || "";
  if (authority === "") {
    fail(`${label} must include a host`);
  }
  if (authority.includes("@")) {
    fail(`${label} must not include userinfo`);
  }

  let host;
  let portText;
  if (authority.startsWith("[")) {
    const close = authority.indexOf("]");
    if (close < 0) {
      fail(`${label} has an unterminated IPv6 host`);
    }
    host = authority.slice(1, close);
    const remainder = authority.slice(close + 1);
    if (remainder !== "" && !remainder.startsWith(":")) {
      fail(`${label} has an invalid authority`);
    }
    portText = remainder === "" ? "" : remainder.slice(1);
    if (!/^[0-9a-f:.]+$/i.test(host)) {
      fail(`${label} has an invalid IPv6 host`);
    }
  } else {
    const colonCount = (authority.match(/:/g) || []).length;
    if (colonCount > 1) {
      fail(`${label} has an unbracketed IPv6 host`);
    }
    const colon = authority.lastIndexOf(":");
    if (colon >= 0) {
      host = authority.slice(0, colon);
      portText = authority.slice(colon + 1);
    } else {
      host = authority;
      portText = "";
    }
    if (host === "" || host.includes(":")) {
      fail(`${label} has an invalid host`);
    }
  }

  host = host.toLowerCase();
  if (host.endsWith(".") || host.includes("%")) {
    fail(`${label} has an invalid host`);
  }

  let port;
  if (portText === "") {
    port = scheme === "https" ? 443 : 80;
  } else {
    if (!/^\d{1,5}$/.test(portText)) {
      fail(`${label} has a non-numeric or invalid port`);
    }
    port = Number(portText);
    if (port < 1 || port > 65535) {
      fail(`${label} port must be between 1 and 65535`);
    }
  }

  if (path === "") {
    path = "/";
  }
  if (path.includes("//") || path.includes("..")) {
    fail(`${label} has an unsafe path`);
  }
  if (allowedPath !== undefined && path !== allowedPath) {
    fail(`${label} path must be exactly ${allowedPath}`);
  }
  if (allowedPath === undefined && path !== "/") {
    fail(`${label} must be an origin without a path`);
  }

  const hostForUrl = host.includes(":") ? `[${host}]` : host;
  const portSuffix = portText === "" ? "" : `:${portText}`;
  const origin = `${scheme}://${hostForUrl}${portSuffix}`;

  return { scheme, host, port, path, origin, url: `${origin}${path === "/" ? "" : path}` };
}

function parseTestHosts(raw) {
  const hosts = new Set();
  const value = String(raw === undefined || raw === null ? "" : raw).trim();
  if (value === "") {
    return hosts;
  }
  for (const part of value.split(",")) {
    const entry = part.trim().toLowerCase();
    if (entry === "") {
      fail("LOADTEST_TEST_HOSTS must not contain empty entries");
    }
    if (entry.includes("*") || entry.includes("/") || entry.includes(":") || entry.includes("@") || entry.includes("?") || entry.includes("#")) {
      fail(`LOADTEST_TEST_HOSTS entry is not a bare hostname: ${entry}`);
    }
    if (IPV4_RE.test(entry) || (!HOSTNAME_RE.test(entry) && !COMPOSE_TEST_HOSTS.has(entry))) {
      fail(`LOADTEST_TEST_HOSTS entry is not a valid test hostname: ${entry}`);
    }
    hosts.add(entry);
  }
  return hosts;
}

function assertAllowedHost(host, targetEnv, testHosts) {
  for (const label of host.split(".")) {
    if (FORBIDDEN_HOST_LABELS.has(label)) {
      fail(`refusing production-looking host label "${label}" in ${host}`);
    }
  }
  if (LOOPBACK_HOSTS.has(host)) {
    return;
  }
  if (LOCAL_ONLY_HOSTS.has(host)) {
    if (targetEnv !== "local") {
      fail(`host ${host} is only allowed for TARGET_ENV=local`);
    }
    return;
  }
  if (targetEnv === "test" && testHosts.has(host)) {
    return;
  }
  fail(`host ${host} is not in the allow-list for TARGET_ENV=${targetEnv}`);
}

export function validateTargetEnv(env) {
  const source = env || {};
  const targetEnv = String(source.TARGET_ENV === undefined || source.TARGET_ENV === null ? "" : source.TARGET_ENV)
    .trim()
    .toLowerCase();
  if (targetEnv === "") {
    fail("TARGET_ENV is required (local|test)");
  }
  if (targetEnv !== "local" && targetEnv !== "test") {
    fail(`TARGET_ENV must be local or test, got "${targetEnv}"`);
  }

  const parsed = parseStrictUrl(source.BASE_URL, "BASE_URL");
  const testHosts = targetEnv === "test" ? parseTestHosts(source.LOADTEST_TEST_HOSTS) : new Set();
  assertAllowedHost(parsed.host, targetEnv, testHosts);

  return Object.freeze({
    targetEnv,
    baseUrl: parsed.origin,
    host: parsed.host,
    port: parsed.port,
    protocol: parsed.scheme,
    origin: parsed.origin,
  });
}

export function validateMetricsUrl(env, target) {
  if (!target || !target.targetEnv) {
    fail("validateMetricsUrl requires the result of validateTargetEnv");
  }
  const source = env || {};
  const parsed = parseStrictUrl(source.METRICS_URL, "METRICS_URL", "/metrics");
  const testHosts = target.targetEnv === "test" ? parseTestHosts(source.LOADTEST_TEST_HOSTS) : new Set();
  assertAllowedHost(parsed.host, target.targetEnv, testHosts);
  return Object.freeze({ url: parsed.url, host: parsed.host, port: parsed.port });
}
