import { afterEach, describe, expect, it, vi } from "vitest";
import { createAuthFetch, createTokenStore, type AuthProvider, type TokenPair } from "./auth";
import { createApiClient, withIdempotencyKey } from "./index";

function failure(code: number, requestId = "server_fail"): Response {
  return new Response(
    JSON.stringify({ ok: false, error: { code, message: { zh: "internal detail", en: "internal detail", km: "internal detail" } }, request_id: requestId }),
    { status: 401, headers: { "Content-Type": "application/json" } }
  );
}

function success(): Response {
  return new Response(JSON.stringify({ ok: true, data: { pong: true }, request_id: "server_ok" }), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

function fakeProvider(overrides: Partial<AuthProvider> = {}): AuthProvider {
  return {
    login: vi.fn<AuthProvider["login"]>(async () => ({ accessToken: "login-at", refreshToken: "login-rt" })),
    refresh: vi.fn<AuthProvider["refresh"]>(async () => ({ accessToken: "new-at", refreshToken: "new-rt" })),
    logout: vi.fn<AuthProvider["logout"]>(),
    ...overrides
  };
}

function requestsOf(mock: ReturnType<typeof vi.fn>): Request[] {
  return mock.mock.calls.map((call) => call[0] as Request);
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("token store", () => {
  it("keeps tokens in memory only and exposes the access token", () => {
    const store = createTokenStore(fakeProvider());
    expect(store.getAccessToken()).toBeNull();
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    expect(store.getAccessToken()).toBe("at");
    store.clear();
    expect(store.getAccessToken()).toBeNull();
  });

  it("logs in through the provider and stores the pair", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    await store.login();
    expect(provider.login).toHaveBeenCalledTimes(1);
    expect(store.getAccessToken()).toBe("login-at");
  });

  it("logs out through the provider and clears the pair", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    await store.logout();
    expect(provider.logout).toHaveBeenCalledTimes(1);
    expect(store.getAccessToken()).toBeNull();
  });

  it("stops notifying a listener after it unsubscribes", async () => {
    const provider = fakeProvider({ refresh: vi.fn<AuthProvider["refresh"]>(async () => { throw new Error("refresh rejected"); }) });
    const store = createTokenStore(provider);
    const listener = vi.fn();
    const unsubscribe = store.onReloginRequired(listener);
    unsubscribe();
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    await store.refreshOnce();
    expect(listener).not.toHaveBeenCalled();
  });
});

describe("single-flight refresh", () => {
  it("refreshes once for 50 concurrent expired requests and replays all of them", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const fetchMock = vi.fn<typeof fetch>(async (input) =>
      (input as Request).headers.get("Authorization") === "Bearer new-at" ? success() : failure(2001, "server_expired")
    );
    const authFetch = createAuthFetch(fetchMock, { store });

    const responses = await Promise.all(
      Array.from({ length: 50 }, (_, index) => authFetch(new Request(`http://example.test/v1/public/ping?i=${index}`)))
    );

    expect(provider.refresh).toHaveBeenCalledTimes(1);
    expect(provider.refresh).toHaveBeenCalledWith("old-rt");
    expect(responses.map((response) => response.status)).toEqual(Array.from({ length: 50 }, () => 200));
    expect(requestsOf(fetchMock as ReturnType<typeof vi.fn>).filter((request) => request.headers.get("Authorization") === "Bearer new-at")).toHaveLength(50);
    expect(store.getAccessToken()).toBe("new-at");
  });

  it("clears the session and fires relogin once when the refresh fails", async () => {
    const provider = fakeProvider({ refresh: vi.fn<AuthProvider["refresh"]>(async () => { throw new Error("refresh token reuse detected"); }) });
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const relogin = vi.fn();
    store.onReloginRequired(relogin);
    const fetchMock = vi.fn<typeof fetch>(async () => failure(2001, "server_expired"));
    const authFetch = createAuthFetch(fetchMock, { store });

    const responses = await Promise.all(
      Array.from({ length: 5 }, (_, index) => authFetch(new Request(`http://example.test/v1/public/ping?i=${index}`)))
    );

    expect(provider.refresh).toHaveBeenCalledTimes(1);
    expect(relogin).toHaveBeenCalledTimes(1);
    expect(store.getAccessToken()).toBeNull();
    expect(responses.map((response) => response.status)).toEqual([401, 401, 401, 401, 401]);
    expect(fetchMock).toHaveBeenCalledTimes(5);
  });

  it("does not call refresh when there is no refresh token", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at" });
    const relogin = vi.fn();
    store.onReloginRequired(relogin);
    const fetchMock = vi.fn<typeof fetch>(async () => failure(2001, "server_expired"));
    const authFetch = createAuthFetch(fetchMock, { store });

    const responses = await Promise.all(
      Array.from({ length: 3 }, (_, index) => authFetch(new Request(`http://example.test/v1/public/ping?i=${index}`)))
    );

    expect(provider.refresh).not.toHaveBeenCalled();
    expect(relogin).toHaveBeenCalledTimes(1);
    expect(responses.map((response) => response.status)).toEqual([401, 401, 401]);
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("replays at most once when the replay is still expired", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const fetchMock = vi.fn<typeof fetch>(async () => failure(2001, "server_expired"));
    const authFetch = createAuthFetch(fetchMock, { store });

    const response = await authFetch(new Request("http://example.test/v1/public/ping"));

    expect(provider.refresh).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(response.status).toBe(401);
  });

  it.each([2003, 2004])("does not refresh on step-up error code %i", async (code) => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const fetchMock = vi.fn<typeof fetch>(async () => failure(code, "server_stepup"));
    const authFetch = createAuthFetch(fetchMock, { store });

    const response = await authFetch(new Request("http://example.test/v1/merchant/products/p1/publish", { method: "POST" }));

    expect(provider.refresh).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(response.status).toBe(401);
  });

  it("does not treat a non-envelope 401 as an expired session", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const fetchMock = vi.fn<typeof fetch>(async () => new Response("gateway text", { status: 401 }));
    const authFetch = createAuthFetch(fetchMock, { store });

    const response = await authFetch(new Request("http://example.test/v1/public/ping"));

    expect(provider.refresh).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(response.status).toBe(401);
  });
});

describe("step-up replay", () => {
  it("replays once with the step-up token and the original idempotency key", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    const stepUp = vi.fn(async () => "stepup-token-1");
    const fetchMock = vi.fn<typeof fetch>(async (input) =>
      (input as Request).headers.get("X-Step-Up-Token") ? success() : failure(2003, "server_stepup")
    );
    const authFetch = createAuthFetch(fetchMock, { store, stepUp });

    const response = await authFetch(
      new Request("http://example.test/v1/merchant/products/p1/publish", {
        method: "POST",
        headers: { "Idempotency-Key": "fixed_key_12345678", "Content-Type": "application/json" },
        body: JSON.stringify({ publish: true })
      })
    );

    const requests = requestsOf(fetchMock as ReturnType<typeof vi.fn>);
    expect(stepUp).toHaveBeenCalledWith({ code: 2003, requestId: "server_stepup" });
    expect(requests).toHaveLength(2);
    expect(requests[0]?.headers.get("X-Step-Up-Token")).toBeNull();
    expect(requests[1]?.headers.get("X-Step-Up-Token")).toBe("stepup-token-1");
    expect(requests[1]?.headers.get("Idempotency-Key")).toBe("fixed_key_12345678");
    expect(await requests[1]?.clone().text()).toBe(JSON.stringify({ publish: true }));
    expect(response.status).toBe(200);
  });

  it("does not replay when the step-up handler is cancelled", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    const stepUp = vi.fn(async () => null);
    const fetchMock = vi.fn<typeof fetch>(async () => failure(2003, "server_stepup"));
    const authFetch = createAuthFetch(fetchMock, { store, stepUp });

    const response = await authFetch(new Request("http://example.test/v1/merchant/products/p1/publish", { method: "POST" }));

    expect(stepUp).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(response.status).toBe(401);
  });

  it("stops after a replay that is rejected with an invalid step-up token", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    const stepUp = vi.fn(async () => "stepup-token-1");
    const fetchMock = vi.fn<typeof fetch>(async (input) =>
      (input as Request).headers.get("X-Step-Up-Token") ? failure(2004, "server_stepup_invalid") : failure(2003, "server_stepup")
    );
    const authFetch = createAuthFetch(fetchMock, { store, stepUp });

    const response = await authFetch(new Request("http://example.test/v1/merchant/products/p1/publish", { method: "POST" }));

    expect(stepUp).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(response.status).toBe(401);
  });

  it("falls through to the error when no step-up handler is injected", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    const fetchMock = vi.fn<typeof fetch>(async () => failure(2003, "server_stepup"));
    const authFetch = createAuthFetch(fetchMock, { store });

    const response = await authFetch(new Request("http://example.test/v1/merchant/products/p1/publish", { method: "POST" }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(response.status).toBe(401);
  });
});

describe("token privacy", () => {
  it("never writes tokens to persistent storage or leaks them onto globalThis", async () => {
    const setItem = vi.fn();
    const cookieWrite = vi.fn();
    const storage = { setItem, getItem: vi.fn(), removeItem: vi.fn(), clear: vi.fn(), key: vi.fn(), length: 0 };
    vi.stubGlobal("localStorage", storage);
    vi.stubGlobal("sessionStorage", storage);
    vi.stubGlobal("document", {
      get cookie(): string { return ""; },
      set cookie(value: string) { cookieWrite(value); }
    });

    const secret: TokenPair = { accessToken: "secret_access_token_value", refreshToken: "secret_refresh_token_value" };
    const provider = fakeProvider({ refresh: vi.fn<AuthProvider["refresh"]>(async () => secret) });
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const fetchMock = vi.fn<typeof fetch>(async (input) =>
      (input as Request).headers.get("Authorization") === `Bearer ${secret.accessToken}` ? success() : failure(2001, "server_expired")
    );

    const response = await createAuthFetch(fetchMock, { store })(new Request("http://example.test/v1/public/ping"));

    expect(response.status).toBe(200);
    expect(setItem).not.toHaveBeenCalled();
    expect(cookieWrite).not.toHaveBeenCalled();
    const descriptors = Object.values(Object.getOwnPropertyDescriptors(globalThis));
    expect(descriptors.some((descriptor) => descriptor.value === secret.accessToken || descriptor.value === secret.refreshToken)).toBe(false);
  });
});

describe("api client wiring", () => {
  it("authenticates from the store and replays the request after a single-flight refresh", async () => {
    const provider = fakeProvider();
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const fetchMock = vi.fn<typeof fetch>(async (input) =>
      (input as Request).headers.get("Authorization") === "Bearer new-at" ? success() : failure(2001, "server_expired")
    );
    const client = createApiClient({ baseUrl: "http://example.test", fetch: fetchMock, auth: { store } });

    const result = await client.GET("/v1/public/ping");

    expect(result.data?.data.pong).toBe(true);
    expect(provider.refresh).toHaveBeenCalledTimes(1);
    expect(requestsOf(fetchMock as ReturnType<typeof vi.fn>)[0]?.headers.get("Authorization")).toBe("Bearer old-at");
  });

  it("keeps the generated idempotency key when a write is replayed after step-up", async () => {
    const store = createTokenStore(fakeProvider());
    store.setTokens({ accessToken: "at", refreshToken: "rt" });
    const stepUp = vi.fn(async () => "stepup-token-2");
    const fetchMock = vi.fn<typeof fetch>(async (input) =>
      (input as Request).headers.get("X-Step-Up-Token") ? success() : failure(2003, "server_stepup")
    );
    const client = createApiClient({ baseUrl: "http://example.test", fetch: fetchMock, auth: { store, stepUp } });

    await client.POST("/v1/buyer/auth/logout", { params: { header: withIdempotencyKey() } });

    const requests = requestsOf(fetchMock as ReturnType<typeof vi.fn>);
    expect(requests).toHaveLength(2);
    expect(requests[0]?.headers.get("Idempotency-Key")).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(requests[1]?.headers.get("Idempotency-Key")).toBe(requests[0]?.headers.get("Idempotency-Key"));
    expect(requests[1]?.headers.get("X-Step-Up-Token")).toBe("stepup-token-2");
  });

  it("still surfaces the typed ApiError when the session cannot be refreshed", async () => {
    const provider = fakeProvider({ refresh: vi.fn<AuthProvider["refresh"]>(async () => { throw new Error("refresh rejected"); }) });
    const store = createTokenStore(provider);
    store.setTokens({ accessToken: "old-at", refreshToken: "old-rt" });
    const relogin = vi.fn();
    store.onReloginRequired(relogin);
    const fetchMock = vi.fn<typeof fetch>(async () => failure(2001, "server_expired"));
    const client = createApiClient({ baseUrl: "http://example.test", fetch: fetchMock, auth: { store } });

    await expect(client.GET("/v1/public/ping")).rejects.toMatchObject({ code: 2001, kind: "unauthenticated", messageKey: "error.2001" });
    expect(relogin).toHaveBeenCalledTimes(1);
  });

  it("rejects mixing the auth store with the legacy getToken option", () => {
    const store = createTokenStore(fakeProvider());
    expect(() => createApiClient({ baseUrl: "http://example.test", getToken: () => "at", auth: { store } })).toThrow();
  });
});
