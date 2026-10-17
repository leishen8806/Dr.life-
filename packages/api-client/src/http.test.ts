import { describe, expect, it, vi } from "vitest";
import {
  CircuitOpenError,
  createResilientFetch,
  createIdempotentAction,
  redactSensitive,
  retryDelay
} from "./http";

const ok = () => new Response("{}", { status: 200 });

describe("resilient fetch", () => {
  it("retries retryable responses with bounded full jitter and Retry-After", async () => {
    expect(retryDelay(1, "2", () => 0.5)).toBeGreaterThanOrEqual(2000);
    const fetch = vi.fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(new Response("", { status: 503 }))
      .mockResolvedValueOnce(ok());
    const client = createResilientFetch(fetch, { retry: { random: () => 0, maxAttempts: 2 } });

    vi.useFakeTimers();
    const result = client(new Request("https://api.test/v1/public/ping"));
    await vi.advanceTimersByTimeAsync(3_000);
    await expect(result).resolves.toMatchObject({ status: 200 });
    vi.useRealTimers();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("does not retry ordinary 4xx responses or a 422 idempotency conflict", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response("", { status: 422 }));
    const client = createResilientFetch(fetch);

    await client(new Request("https://api.test/v1/public/ping"));
    expect(fetch).toHaveBeenCalledOnce();
  });

  it("enforces the attempt and elapsed-time retry limits", async () => {
    const attempts = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response("", { status: 503 }));
    await createResilientFetch(attempts, { retry: { maxAttempts: 2, random: () => 0 } })(new Request("https://api.test/v1/catalog"));
    expect(attempts).toHaveBeenCalledTimes(2);

    const elapsed = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response("", { status: 503 }));
    const times = [0, 0, 15_000];
    await createResilientFetch(elapsed, { now: () => times.shift() ?? 15_000, retry: { random: () => 0 } })(new Request("https://api.test/v1/catalog"));
    expect(elapsed).toHaveBeenCalledOnce();
  });

  it("reuses the generated idempotency key across a write retry", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(new Response("", { status: 503 }))
      .mockResolvedValueOnce(ok());
    const client = createResilientFetch(fetch, { retry: { random: () => 0, maxAttempts: 2 } });

    await client(new Request("https://api.test/v1/orders", { method: "POST", body: "{}" }));
    const [first, second] = fetch.mock.calls.map(([request]) => request as Request) as [Request, Request];
    expect(first.headers.get("Idempotency-Key")).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    expect(second.headers.get("Idempotency-Key")).toBe(first.headers.get("Idempotency-Key"));
  });

  it("does not retry a caller-cancelled write", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation((request) => new Promise<Response>((_resolve, reject) => {
      (request as Request).signal.addEventListener("abort", () => reject((request as Request).signal.reason));
    }));
    const controller = new AbortController();
    const pending = createResilientFetch(fetch)(new Request("https://api.test/v1/orders", { method: "POST", body: "{}", signal: controller.signal }));
    await Promise.resolve();
    controller.abort(new DOMException("Cancelled", "AbortError"));
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(fetch).toHaveBeenCalledOnce();
  });

  it("replaces an invalid caller request ID", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(ok());
    await createResilientFetch(fetch)(new Request("https://api.test/v1/catalog", { headers: { "X-Request-ID": "unsafe id" } }));
    expect((fetch.mock.calls[0]?.[0] as Request).headers.get("X-Request-ID")).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
  });

  it("opens breakers per endpoint category and half-open success restores it", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(new Response("", { status: 503 }))
      .mockResolvedValueOnce(ok())
      .mockResolvedValueOnce(ok());
    let now = 0;
    const client = createResilientFetch(fetch, { breaker: { failureThreshold: 1, cooldownMs: 10 }, retry: { maxAttempts: 1 }, now: () => now });

    await client(new Request("https://api.test/v1/orders"));
    await expect(client(new Request("https://api.test/v1/orders"))).rejects.toBeInstanceOf(CircuitOpenError);
    await expect(client(new Request("https://api.test/v1/catalog"))).resolves.toMatchObject({ status: 200 });
    now = 11;
    await expect(client(new Request("https://api.test/v1/orders"))).resolves.toMatchObject({ status: 200 });
  });

  it("deduplicates concurrent GETs by URL and language but never writes", async () => {
    let resolve!: (value: Response) => void;
    let pending = true;
    const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(
      () => pending ? new Promise<Response>((done) => { resolve = done; }) : Promise.resolve(ok())
    );
    const client = createResilientFetch(fetch);
    const requests = Array.from({ length: 100 }, () => client(new Request("https://api.test/v1/catalog", { headers: { "Accept-Language": "en" } })));
    await Promise.resolve();
    expect(fetch).toHaveBeenCalledOnce();
    resolve(ok());
    pending = false;
    await Promise.all(requests);
    await client(new Request("https://api.test/v1/catalog", { headers: { "Accept-Language": "km" } }));
    await client(new Request("https://api.test/v1/orders", { method: "POST", body: "{}" }));
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it("keeps an action key stable and redacts sensitive log values", async () => {
    const action = createIdempotentAction();
    expect(action.request({ amount: 1 }).headers["Idempotency-Key"]).toBe(action.request({ amount: 1 }).headers["Idempotency-Key"]);
    expect(() => action.request({ amount: 2 })).toThrow(/different request body/);
    expect(() => action.request(new URLSearchParams() as never)).toThrow(/JSON request bodies/);
    expect(redactSensitive("Bearer abc initData=secret +85512345678")).not.toMatch(/abc|secret|85512345678/);
  });
});
