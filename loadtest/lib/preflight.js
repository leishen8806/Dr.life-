export class PreflightError extends Error {
  constructor(message) {
    super(message);
    this.name = "PreflightError";
  }
}

function fail(message) {
  throw new PreflightError(message);
}

export function requireEnv(env, name) {
  const source = env || {};
  const raw = source[name];
  const value = String(raw === undefined || raw === null ? "" : raw).trim();
  if (value === "") {
    fail(`${name} is required`);
  }
  return value;
}

export function requireOneOf(env, name, allowed) {
  const value = requireEnv(env, name).toLowerCase();
  const normalized = allowed.map((item) => String(item).toLowerCase());
  if (!normalized.includes(value)) {
    fail(`${name} must be one of ${allowed.join("|")}, got "${value}"`);
  }
  return value;
}

export function parseMetrics(text) {
  if (typeof text !== "string") {
    fail("metrics response body is missing");
  }
  const values = new Map();
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (line === "" || line.startsWith("#")) {
      continue;
    }
    const parts = line.split(/\s+/);
    if (parts.length < 2) {
      continue;
    }
    const sample = parts[0];
    const brace = sample.indexOf("{");
    const name = brace >= 0 ? sample.slice(0, brace) : sample;
    if (name === "") {
      continue;
    }
    const value = Number(parts[1]);
    if (values.has(name) && (!Number.isFinite(values.get(name)) || !Number.isFinite(value))) {
      values.set(name, Number.NaN);
    } else if (values.has(name)) {
      values.set(name, values.get(name) + value);
    } else {
      values.set(name, value);
    }
  }
  return values;
}

export function requireMetrics(text, requiredNames) {
  if (!Array.isArray(requiredNames) || requiredNames.length === 0) {
    fail("requireMetrics needs at least one required metric name");
  }
  const metrics = parseMetrics(text);
  const missing = requiredNames.filter((name) => !metrics.has(name));
  if (missing.length > 0) {
    fail(`missing required metrics: ${missing.join(", ")}`);
  }
  const nonFinite = requiredNames.filter((name) => !Number.isFinite(metrics.get(name)));
  if (nonFinite.length > 0) {
    fail(`required metrics are not finite: ${nonFinite.join(", ")}`);
  }
  return metrics;
}

export function requireMetricFinite(text, name) {
  const metrics = parseMetrics(text);
  if (!metrics.has(name)) {
    fail(`missing required metric: ${name}`);
  }
  const value = metrics.get(name);
  if (!Number.isFinite(value)) {
    fail(`metric ${name} is not finite`);
  }
  return value;
}

export function requireMetricAtLeast(text, name, minimum) {
  if (!Number.isFinite(minimum)) {
    fail("requireMetricAtLeast needs a finite minimum");
  }
  const value = requireMetricFinite(text, name);
  if (value < minimum) {
    fail(`metric ${name} is ${value}, expected at least ${minimum}`);
  }
  return value;
}
