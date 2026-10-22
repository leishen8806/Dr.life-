import { requirePath } from "./scenario.js";

export const DEFAULT_PATHS = Object.freeze({
  health: "/healthz",
  ready: "/readyz",
  ping: "/v1/public/ping",
  protectedProbe: "/v1/buyer/loadtest-probe",
  unknownAudience: "/v1/loadtest-unknown-audience",
  login: "/v1/public/auth/telegram",
  mixedRead: "/v1/public/ping",
  mixedWrite: "/v1/buyer/cart/items",
  mixedHeavy: "/v1/public/reports/summary",
  withdrawal: "/v1/merchant/withdrawals",
  ledgerEntry: "/v1/ledger/entries",
  cacheProbe: "/v1/public/catalog/hot",
  notify: "/v1/merchant/notifications",
  critical: "/v1/buyer/orders",
  rateLimitProbe: "/v1/public/ping",
});

export function pathFor(env, envName, fallback) {
  const source = env || {};
  if (source[envName] === undefined || source[envName] === null || String(source[envName]).trim() === "") {
    return fallback;
  }
  return requirePath(source[envName], envName);
}

export function probePaths(env) {
  const source = env || {};
  return {
    health: pathFor(source, "HEALTH_PATH", DEFAULT_PATHS.health),
    ready: pathFor(source, "READY_PATH", DEFAULT_PATHS.ready),
    ping: pathFor(source, "PING_PATH", DEFAULT_PATHS.ping),
    protectedProbe: pathFor(source, "PROTECTED_PROBE_PATH", DEFAULT_PATHS.protectedProbe),
    unknownAudience: pathFor(source, "UNKNOWN_AUDIENCE_PATH", DEFAULT_PATHS.unknownAudience),
    login: pathFor(source, "LOGIN_PATH", DEFAULT_PATHS.login),
    mixedRead: pathFor(source, "MIX_READ_PATH", DEFAULT_PATHS.mixedRead),
    mixedWrite: pathFor(source, "MIX_WRITE_PATH", DEFAULT_PATHS.mixedWrite),
    mixedHeavy: pathFor(source, "MIX_HEAVY_PATH", DEFAULT_PATHS.mixedHeavy),
    withdrawal: pathFor(source, "WITHDRAWAL_PATH", DEFAULT_PATHS.withdrawal),
    ledgerEntry: pathFor(source, "LEDGER_ENTRY_PATH", DEFAULT_PATHS.ledgerEntry),
    cacheProbe: pathFor(source, "CACHE_PROBE_PATH", DEFAULT_PATHS.cacheProbe),
    notify: pathFor(source, "NOTIFY_PATH", DEFAULT_PATHS.notify),
    critical: pathFor(source, "CRITICAL_PATH", DEFAULT_PATHS.critical),
    rateLimitProbe: pathFor(source, "RATE_LIMIT_PROBE_PATH", DEFAULT_PATHS.rateLimitProbe),
  };
}