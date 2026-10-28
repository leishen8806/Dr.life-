import { describe, expect, it, vi } from "vitest";
import { createApiClient, withIdempotencyKey } from "./index";

describe("api client", () => {
  it("calls the typed ping endpoint and adds standard headers", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, data: { pong: true }, request_id: "server_1" }), { status: 200, headers: { "Content-Type": "application/json" } })
    );
    const client = createApiClient({ baseUrl: "http://example.test", fetch: fetchMock, getToken: () => "test-token", getLanguage: () => "km" });

    const result = await client.GET("/v1/public/ping");
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(result.data?.data.pong).toBe(true);
    expect(request.headers.get("Authorization")).toBe("Bearer test-token");
    expect(request.headers.get("Accept-Language")).toBe("km");
    expect(request.headers.get("X-Request-ID")).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
  });

  it("creates a legal idempotency key", () => {
    expect(withIdempotencyKey()["Idempotency-Key"]).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
  });

  it("turns an error envelope into a safe typed ApiError", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ ok: false, error: { code: 2001, message: { zh: "internal detail", en: "internal detail", km: "internal detail" } }, request_id: "server_2" }), { status: 401, headers: { "Content-Type": "application/json" } })
    );
    const client = createApiClient({ baseUrl: "http://example.test", fetch: fetchMock });

    await expect(client.GET("/v1/public/ping")).rejects.toMatchObject({
      code: 2001,
      requestId: "server_2",
      status: 401,
      kind: "unauthenticated",
      messageKey: "error.2001"
    });
    await expect(client.GET("/v1/public/ping")).rejects.not.toThrow("internal detail");
  });

  it("carries the login lockout window from Retry-After into the ApiError", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ ok: false, error: { code: 2007, message: { zh: "internal detail", en: "internal detail", km: "internal detail" } }, request_id: "server_3" }), { status: 429, headers: { "Content-Type": "application/json", "Retry-After": "30" } })
    );
    const client = createApiClient({ baseUrl: "http://example.test", fetch: fetchMock, resilience: { retry: { maxAttempts: 1 } } });

    await expect(client.GET("/v1/public/ping")).rejects.toMatchObject({
      code: 2007,
      status: 429,
      kind: "loginThrottled",
      messageKey: "error.2007",
      retryAfterSeconds: 30
    });
  });
});
