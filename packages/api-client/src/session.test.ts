import { describe, expect, it, vi } from "vitest";
import { createTelegramSession } from "./session";

const publicId = "01JABCDEFGHJKMNPQRSTVZ0123";
const baseUrl = "http://example.test";

function reply(
  body: unknown,
  status = 200,
  headers: Record<string, string> = {}
): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers }
  });
}

function loginData(lang: string | null, needsLanguage: boolean) {
  return {
    ok: true,
    data: {
      access_token: "access-1",
      access_expires_in: 900,
      refresh_token: "refresh-1",
      refresh_expires_in: 2_592_000,
      account: { public_id: publicId, role: "buyer", lang, status: "active" },
      needs_language: needsLanguage,
      is_new_account: needsLanguage
    },
    request_id: "req_login"
  };
}

function session(fetchMock: typeof fetch, end: "buyer" | "merchant" = "buyer") {
  return createTelegramSession({
    end,
    baseUrl,
    fetch: fetchMock,
    initData: () => "query_id=AAA&hash=BBB",
    getLanguage: () => "km"
  });
}

describe("createTelegramSession", () => {
  it("replays initData for its own end and normalises the account", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(reply(loginData(null, true)));

    const account = await session(fetchMock, "merchant").login();
    const request = fetchMock.mock.calls[0]?.[0] as Request;

    expect(request.url).toBe(`${baseUrl}/v1/public/auth/telegram`);
    expect(await request.clone().json()).toMatchObject({
      end: "merchant",
      init_data: "query_id=AAA&hash=BBB"
    });
    // A `security: []` path gets no bearer token, even once the store holds one.
    expect(request.headers.get("Authorization")).toBeNull();
    expect(account).toEqual({
      publicId,
      role: "buyer",
      lang: null,
      status: "active",
      needsLanguage: true
    });
  });

  it("keeps a registered language and a frozen status from the login response", async () => {
    const frozen = loginData("km", false);
    frozen.data.account.status = "frozen";
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(reply(frozen));

    await expect(session(fetchMock).login()).resolves.toMatchObject({
      lang: "km",
      status: "frozen",
      needsLanguage: false
    });
  });

  it("reports a blocked account as 2006 with no retry window", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      reply(
        { ok: false, error: { code: 2006, message: { zh: "x", en: "x", km: "x" } }, request_id: "req_blocked" },
        403,
        { "Retry-After": "60" }
      )
    );

    await expect(session(fetchMock).login()).rejects.toMatchObject({
      code: 2006,
      kind: "accountBlocked",
      retryAfterSeconds: undefined
    });
  });

  // A window shorter than the retry budget is the case that matters: the resilient fetch
  // stamps writes with an idempotency key, so without the session's own retry limit it
  // would wait out the lockout and retry, and the user would never see the countdown.
  it("carries the 2007 lockout window and does not retry the login by itself", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      reply(
        { ok: false, error: { code: 2007, message: { zh: "x", en: "x", km: "x" } }, request_id: "req_throttled" },
        429,
        { "Retry-After": "1" }
      )
    );

    await expect(session(fetchMock).login()).rejects.toMatchObject({
      code: 2007,
      kind: "loginThrottled",
      retryAfterSeconds: 1
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("registers the language on the end's own path with a bearer token and an idempotency key", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(reply(loginData(null, true)))
      .mockResolvedValueOnce(
        reply({
          ok: true,
          data: { public_id: publicId, role: "merchant", lang: "km", needs_language: false, status: "active" },
          request_id: "req_language"
        })
      );

    const current = session(fetchMock, "merchant");
    await current.login();
    const account = await current.registerLanguage("km");
    const request = fetchMock.mock.calls[1]?.[0] as Request;

    expect(request.method).toBe("PUT");
    expect(request.url).toBe(`${baseUrl}/v1/merchant/me/language`);
    expect(await request.clone().json()).toEqual({ lang: "km" });
    expect(request.headers.get("Authorization")).toBe("Bearer access-1");
    expect(request.headers.get("Idempotency-Key")).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(account).toMatchObject({ lang: "km", needsLanguage: false });
  });

  it("refuses to invent an account when the envelope carries no payload", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }));

    await expect(session(fetchMock).login()).rejects.toThrow(
      "Session request returned no payload"
    );
  });
});
