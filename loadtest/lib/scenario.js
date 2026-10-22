export class ScenarioConfigError extends Error {
  constructor(message) {
    super(message);
    this.name = "ScenarioConfigError";
  }
}

function fail(message) {
  throw new ScenarioConfigError(message);
}

function readRaw(env, name, fallback) {
  const source = env || {};
  if (source[name] === undefined || source[name] === null) {
    return fallback === undefined ? "" : String(fallback);
  }
  return String(source[name]).trim();
}

function requireInteger(value, name, minimum) {
  if (!/^\d+$/.test(value)) {
    fail(`${name} must be a non-negative integer, got "${value}"`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < minimum) {
    fail(`${name} must be an integer >= ${minimum}, got "${value}"`);
  }
  return parsed;
}

export function parseWeights(env) {
  const read = requireInteger(readRaw(env, "MIX_READ_WEIGHT", "85"), "MIX_READ_WEIGHT", 0);
  const write = requireInteger(readRaw(env, "MIX_WRITE_WEIGHT", "10"), "MIX_WRITE_WEIGHT", 0);
  const heavy = requireInteger(readRaw(env, "MIX_HEAVY_WEIGHT", "5"), "MIX_HEAVY_WEIGHT", 0);
  const total = read + write + heavy;
  if (total !== 100) {
    fail(`MIX_READ_WEIGHT + MIX_WRITE_WEIGHT + MIX_HEAVY_WEIGHT must equal 100, got ${total}`);
  }
  return { read, write, heavy };
}

export function parseDurationSeconds(env, name, fallback) {
  const raw = readRaw(env, name, fallback);
  if (raw === "") {
    fail(`${name} must not be blank`);
  }
  const match = /^([1-9]\d*)(s|m|h)$/.exec(raw);
  if (!match) {
    fail(`${name} must be a positive k6 duration such as 30s, 10m, or 1h, got "${raw}"`);
  }
  const value = Number(match[1]);
  const factor = match[2] === "s" ? 1 : match[2] === "m" ? 60 : 3600;
  const seconds = value * factor;
  if (!Number.isSafeInteger(seconds) || seconds <= 0) {
    fail(`${name} is out of range: "${raw}"`);
  }
  return seconds;
}

export function parsePositiveInt(env, name, fallback) {
  const raw = readRaw(env, name, fallback);
  if (!/^[1-9]\d*$/.test(raw)) {
    fail(`${name} must be a positive integer, got ${raw}`);
  }
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value < 1) {
    fail(`${name} must be a positive safe integer, got ${raw}`);
  }
  return value;
}

export function parseRatio(env, name, fallback) {
  const raw = readRaw(env, name, fallback);
  if (!/^(?:0(?:\.\d+)?|1(?:\.0+)?)$/.test(raw)) {
    fail(`${name} must be a ratio between 0 and 1, got ${raw}`);
  }
  return Number(raw);
}

export function assertRecovered(initial, recovered, options) {
  const settings = options || {};
  const label = settings.label || "metric";
  const maxRatio = settings.maxRatio === undefined ? 1.25 : settings.maxRatio;
  const maxAbsoluteGrowth = settings.maxAbsoluteGrowth === undefined ? 0 : settings.maxAbsoluteGrowth;
  if (![initial, recovered].every((value) => typeof value === "number" && Number.isFinite(value) && value >= 0)) {
    fail(`${label} needs finite non-negative before/after samples`);
  }
  if (!Number.isFinite(maxRatio) || maxRatio <= 0 || !Number.isFinite(maxAbsoluteGrowth) || maxAbsoluteGrowth < 0) {
    fail(`${label} has invalid recovery bounds`);
  }
  const ceiling = initial * maxRatio + maxAbsoluteGrowth;
  if (recovered > ceiling) {
    fail(`${label} did not recover: before ${initial}, after ${recovered}, ceiling ${ceiling}`);
  }
  return { initial, recovered, ceiling };
}

export function requirePath(value, name) {
  const raw = value === undefined || value === null ? "" : String(value);
  if (raw === "") {
    fail(`${name} is required`);
  }
  if (raw !== raw.trim()) {
    fail(`${name} must not contain leading or trailing whitespace`);
  }
  if (!raw.startsWith("/")) {
    fail(`${name} must start with /, got "${raw}"`);
  }
  if (raw === "/") {
    fail(`${name} must not be the bare root path`);
  }
  if (raw.includes("//") || raw.includes("..") || raw.includes("?") || raw.includes("#") || raw.includes("\\") || /\s/.test(raw)) {
    fail(`${name} must be a single safe path without traversal, query, fragment, or whitespace: "${raw}"`);
  }
  return raw;
}

export function parsePathList(value, name) {
  const raw = value === undefined || value === null ? "" : String(value).trim();
  if (raw === "") {
    fail(`${name} is required`);
  }
  const paths = raw.split(",").map((item) => item.trim());
  if (paths.some((item) => item === "")) {
    fail(`${name} must not contain empty entries`);
  }
  return paths.map((item) => requirePath(item, name));
}

export function parseRetryAfter(value) {
  const raw = value === undefined || value === null ? "" : String(value).trim();
  if (!/^[1-9]\d*$/.test(raw)) {
    fail(`Retry-After must be a positive integer number of seconds, got "${raw}"`);
  }
  const seconds = Number(raw);
  if (!Number.isSafeInteger(seconds)) {
    fail(`Retry-After is out of range: "${raw}"`);
  }
  return seconds;
}

function average(values) {
  let total = 0;
  for (const value of values) {
    total += value;
  }
  return total / values.length;
}

export function evaluateTrend(samples, options) {
  const settings = options || {};
  const label = settings.label || "series";
  const maxRatio = settings.maxRatio === undefined ? 1.5 : settings.maxRatio;
  const minGrowth = settings.minGrowth === undefined ? 0 : settings.minGrowth;
  if (!Array.isArray(samples) || samples.length < 4) {
    fail(`${label} needs at least 4 samples to evaluate a trend`);
  }
  for (const sample of samples) {
    if (typeof sample !== "number" || !Number.isFinite(sample) || sample < 0) {
      fail(`${label} samples must be finite non-negative numbers`);
    }
  }
  if (!Number.isFinite(maxRatio) || maxRatio <= 0) {
    fail(`${label} maxRatio must be a positive finite number`);
  }
  if (!Number.isFinite(minGrowth) || minGrowth < 0) {
    fail(`${label} minGrowth must be a non-negative finite number`);
  }
  const half = Math.floor(samples.length / 2);
  const firstAvg = average(samples.slice(0, half));
  const lastAvg = average(samples.slice(samples.length - half));
  const ratio = firstAvg === 0 ? (lastAvg === 0 ? 1 : Number.POSITIVE_INFINITY) : lastAvg / firstAvg;
  const leak = ratio > maxRatio && lastAvg - firstAvg > minGrowth;
  if (leak) {
    fail(`${label} trends upward: first-half average ${firstAvg}, second-half average ${lastAvg}, ratio ${ratio}`);
  }
  return { firstAvg, lastAvg, ratio, leak: false };
}