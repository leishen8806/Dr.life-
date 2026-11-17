import { afterEach, describe, expect, it, vi } from "vitest";
import { requestForTest } from "../src/api";

afterEach(() => vi.restoreAllMocks());

describe("admin request refresh", () => {
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
});
