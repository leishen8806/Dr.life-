import { afterEach, describe, expect, it, vi } from "vitest";
import { requestForTest } from "../src/api";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllEnvs(); });

describe("admin request refresh", () => {
  it("uses the staging API origin when the build variable is set", async () => {
    vi.stubEnv("VITE_API_BASE_URL", "https://test-api.drlifeplus.com/");
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { ok: true } }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(requestForTest<{ ok: boolean }>("/v1/admin/audit")).resolves.toEqual({ ok: true });
    expect(fetchMock.mock.calls[0][0]).toBe("https://test-api.drlifeplus.com/v1/admin/audit");
  });

  it("refreshes once and retries a 401 request", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: { access_token: "fresh" } }), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: { ok: true } }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(requestForTest<{ ok: boolean }>("/v1/admin/audit")).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(new Headers(fetchMock.mock.calls[2][1].headers).get("Authorization")).toBe("Bearer fresh");
  });

  it("clears the session and emits expired after refresh fails", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 401 }));
    vi.stubGlobal("fetch", fetchMock);
    const expired = vi.fn();
    window.addEventListener("drlife-auth-expired", expired);
    await expect(requestForTest("/v1/admin/audit")).rejects.toMatchObject({ status: 401 });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(expired).toHaveBeenCalledOnce();
    window.removeEventListener("drlife-auth-expired", expired);
  });

  it("does not refresh again when the one retry is still unauthorized", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: { access_token: "fresh" } }), { status: 200 }))
      .mockResolvedValueOnce(new Response(null, { status: 401 }));
    vi.stubGlobal("fetch", fetchMock);
    const expired = vi.fn();
    window.addEventListener("drlife-auth-expired", expired);
    await expect(requestForTest("/v1/admin/audit")).rejects.toMatchObject({ status: 401 });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(expired).not.toHaveBeenCalled();
    window.removeEventListener("drlife-auth-expired", expired);
  });

  it("preserves a non-auth error from the one retry", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: { access_token: "fresh" } }), { status: 200 }))
      .mockResolvedValueOnce(new Response(null, { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);
    const expired = vi.fn();
    window.addEventListener("drlife-auth-expired", expired);
    await expect(requestForTest("/v1/admin/audit")).rejects.toMatchObject({ status: 500 });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(expired).not.toHaveBeenCalled();
    window.removeEventListener("drlife-auth-expired", expired);
  });
});
