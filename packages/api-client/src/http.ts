export type EndpointCategory = string;

export interface RetryOptions {
  maxAttempts?: number;
  maxElapsedMs?: number;
  random?: () => number;
}

export interface BreakerOptions {
  failureThreshold?: number;
  cooldownMs?: number;
  maxCooldownMs?: number;
}

export interface ResilientFetchOptions {
  retry?: RetryOptions;
  breaker?: BreakerOptions;
  timeout?: Partial<TimeoutOptions>;
  now?: () => number;
}

export interface TimeoutOptions { read: number; write: number; heavy: number }

const identifierPattern = /^[A-Za-z0-9_-]{8,64}$/;
const defaultRetry = { maxAttempts: 3, maxElapsedMs: 15_000 };
const defaultBreaker = { failureThreshold: 5, cooldownMs: 10_000, maxCooldownMs: 60_000 };
const defaultTimeout: TimeoutOptions = { read: 8_000, write: 12_000, heavy: 30_000 };
type ResolvedRetry = { maxAttempts: number; maxElapsedMs: number; random: () => number };

export class CircuitOpenError extends Error {
  readonly messageKey = "error.circuitOpen";
  constructor(public readonly category: EndpointCategory) {
    super("Request temporarily unavailable");
    this.name = "CircuitOpenError";
  }
}

export function createIdentifier(): string {
  const value = globalThis.crypto?.randomUUID().replaceAll("-", "_");
  if (!value || !identifierPattern.test(value)) throw new Error("Unable to create request identifier");
  return value;
}

export function retryDelay(attempt: number, retryAfter: string | null, random = Math.random, now = Date.now()): number {
  const fullJitter = Math.floor(random() * Math.min(1_000 * 2 ** (attempt - 1), 8_000));
  const retryAfterMs = parseRetryAfter(retryAfter, now);
  return Math.max(fullJitter, retryAfterMs) + (retryAfterMs ? Math.floor(random() * 250) : 0);
}

function parseRetryAfter(value: string | null, now: number): number {
  if (!value) return 0;
  const seconds = Number(value);
  if (Number.isFinite(seconds) && seconds >= 0) return seconds * 1_000;
  const date = Date.parse(value);
  return Number.isNaN(date) ? 0 : Math.max(0, date - now);
}

function categoryOf(url: string): EndpointCategory {
  const part = new URL(url).pathname.split("/").filter(Boolean);
  return part[0] === "v1" ? (part[1] ?? "default") : (part[0] ?? "default");
}

function isWrite(method: string): boolean {
  return !["GET", "HEAD", "OPTIONS"].includes(method);
}

function isRetryable(response: Response): boolean {
  return response.status === 408 || response.status === 429 || response.status === 503 ||
    (response.status === 409 && response.headers.has("Retry-After"));
}

interface BreakerState { failures: number; openUntil: number; probe: boolean }

class CircuitBreaker {
  private readonly states = new Map<EndpointCategory, BreakerState>();
  constructor(private readonly options: Required<BreakerOptions>, private readonly now: () => number) {}

  allow(category: EndpointCategory): void {
    const state = this.states.get(category);
    if (!state || !state.openUntil) return;
    if (state.openUntil > this.now() || state.probe) throw new CircuitOpenError(category);
    state.probe = true;
  }

  success(category: EndpointCategory): void { this.states.delete(category); }

  failure(category: EndpointCategory): void {
    const state = this.states.get(category) ?? { failures: 0, openUntil: 0, probe: false };
    state.failures += 1;
    state.probe = false;
    if (state.failures >= this.options.failureThreshold) {
      const periods = state.failures - this.options.failureThreshold;
      state.openUntil = this.now() + Math.min(this.options.cooldownMs * 2 ** periods, this.options.maxCooldownMs);
    }
    this.states.set(category, state);
  }
}

export interface IdempotentAction {
  request<T extends JsonValue>(body: T): { headers: { "Idempotency-Key": string }; body: T };
  reset(): void;
}

type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };

export function createIdempotentAction(): IdempotentAction {
  let key = createIdentifier();
  let bodyFingerprint: string | undefined;
  return {
    request: (body) => {
      const next = fingerprint(body);
      if (bodyFingerprint !== undefined && bodyFingerprint !== next)
        throw new Error("Idempotency key cannot be reused with a different request body");
      bodyFingerprint = next;
      return { headers: { "Idempotency-Key": key }, body: JSON.parse(next) as typeof body };
    },
    reset: () => { key = createIdentifier(); bodyFingerprint = undefined; }
  };
}

function fingerprint(value: unknown): string {
  if (value === null || typeof value === "string" || typeof value === "boolean") return JSON.stringify(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new TypeError("Idempotent actions require JSON request bodies");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map(fingerprint).join(",")}]`;
  if (typeof value === "object" && Object.getPrototypeOf(value) === Object.prototype)
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${fingerprint((value as Record<string, unknown>)[key])}`).join(",")}}`;
  throw new TypeError("Idempotent actions require JSON request bodies");
}

export function loginStormDelay(enabled = false, random = Math.random): number {
  return enabled ? Math.floor(random() * 2_001) : 0;
}

export function redactSensitive(value: string): string {
  return value
    .replace(/Bearer\s+[^\s]+/gi, "Bearer [REDACTED]")
    .replace(/(authorization|token|initData)\s*[=:]\s*[^\s&]+/gi, "$1=[REDACTED]")
    .replace(/\+?\d[\d\s-]{6,}\d/g, "[REDACTED_PHONE]");
}

export function createResilientFetch(fetchFn: typeof globalThis.fetch, options: ResilientFetchOptions = {}): typeof globalThis.fetch {
  const retry: ResolvedRetry = { ...defaultRetry, random: Math.random, ...options.retry };
  const timeout = { ...defaultTimeout, ...options.timeout };
  const breaker = new CircuitBreaker({ ...defaultBreaker, ...options.breaker }, options.now ?? Date.now);
  const inflight = new Map<string, Promise<Response>>();
  const now = options.now ?? Date.now;

  return async (input, init) => {
    const original = new Request(input, init);
    const headers = new Headers(original.headers);
    const method = original.method.toUpperCase();
    if (!identifierPattern.test(headers.get("X-Request-ID") ?? "")) headers.set("X-Request-ID", createIdentifier());
    if (isWrite(method) && !headers.has("Idempotency-Key")) headers.set("Idempotency-Key", createIdentifier());
    const request = new Request(original, { headers });
    const dedupeKey = method === "GET" ? `${request.url}|${headers.get("Accept-Language") ?? "en"}` : undefined;
    if (dedupeKey && inflight.has(dedupeKey)) return (await inflight.get(dedupeKey)!).clone();
    const work = requestWithRetry(request, fetchFn, retry, breaker, timeout, now);
    if (dedupeKey) inflight.set(dedupeKey, work);
    try { return await work; } finally { if (dedupeKey) inflight.delete(dedupeKey); }
  };
}

async function requestWithRetry(
  request: Request,
  fetchFn: typeof globalThis.fetch,
  retry: ResolvedRetry,
  breaker: CircuitBreaker,
  timeout: TimeoutOptions,
  now: () => number
): Promise<Response> {
  const category = categoryOf(request.url);
  const started = now();
  const deadline = started + retry.maxElapsedMs;
  for (let attempt = 1; ; attempt += 1) {
    if (now() >= deadline) throw new DOMException("Request timed out", "TimeoutError");
    breaker.allow(category);
    try {
      const response = await fetchWithTimeout(fetchFn, request, timeout, deadline - now());
      if (response.status >= 500) breaker.failure(category); else breaker.success(category);
      if (!canRetry(request, response, attempt, retry, started, now)) return response;
      const delay = retryDelay(attempt, response.headers.get("Retry-After"), retry.random, now());
      if (now() + delay >= deadline) return response;
      await wait(delay);
    } catch (error) {
      if (request.signal.aborted) throw error;
      breaker.failure(category);
      if (attempt >= retry.maxAttempts || now() >= deadline) throw error;
      const delay = retryDelay(attempt, null, retry.random, now());
      if (now() + delay >= deadline) throw error;
      await wait(delay);
    }
  }
}

function canRetry(request: Request, response: Response, attempt: number, retry: ResolvedRetry, started: number, now: () => number): boolean {
  return (request.method === "GET" || request.headers.has("Idempotency-Key")) && isRetryable(response) &&
    attempt < retry.maxAttempts && now() - started < retry.maxElapsedMs;
}

function wait(ms: number): Promise<void> {
  return ms ? new Promise((resolve) => setTimeout(resolve, ms)) : Promise.resolve();
}

async function fetchWithTimeout(fetchFn: typeof globalThis.fetch, request: Request, timeout: TimeoutOptions, remainingMs: number): Promise<Response> {
  const controller = new AbortController();
  const abort = () => controller.abort(request.signal.reason);
  request.signal.addEventListener("abort", abort, { once: true });
  const categoryTimeout = request.headers.get("X-Request-Class") === "heavy" ? timeout.heavy : isWrite(request.method) ? timeout.write : timeout.read;
  const ms = Math.min(categoryTimeout, remainingMs);
  let rejectTimeout!: (reason?: unknown) => void;
  const timedOut = new Promise<Response>((_resolve, reject) => { rejectTimeout = reject; });
  const timer = setTimeout(() => {
    const error = new DOMException("Request timed out", "TimeoutError");
    controller.abort(error);
    rejectTimeout(error);
  }, ms);
  try {
    return await Promise.race([fetchFn(new Request(request.clone(), { signal: controller.signal })), timedOut]);
  } finally {
    clearTimeout(timer);
    request.signal.removeEventListener("abort", abort);
  }
}
