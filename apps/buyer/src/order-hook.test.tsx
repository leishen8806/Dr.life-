// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Language } from "@drlife/i18n";
import { type BuyerApiClient, createBuyerApiClient } from "./api";
import { useOrderDetail, useOrderFeed } from "./app";

afterEach(cleanup);

function order(id: string) {
  return {
    public_id: id,
    created_at: "2026-11-10T00:00:00Z",
    payable_amount_e4: 10_000,
    goods_amount_e4: 10_000,
    shipping_amount_e4: 0,
    payment_status: "succeeded",
    suborders: [{
      public_id: `${id}-sub`,
      status: "paid",
      goods_amount_e4: 10_000,
      payable_amount_e4: 10_000,
      shipping_amount_e4: 0,
      shipping_quote_expires_at: "2026-11-11T00:00:00Z",
      items: [{ sku_public_id: `${id}-sku`, quantity: 1, unit_price_e4: 10_000, line_total_e4: 10_000 }]
    }]
  };
}

function page(items: ReturnType<typeof order>[], next_cursor: string | null): Response {
  return new Response(JSON.stringify({ ok: true, data: { items, next_cursor }, request_id: "req_test" }), {
    status: 200,
    headers: { "content-type": "application/json" }
  });
}

function detail(value: ReturnType<typeof order>): Response {
  return new Response(JSON.stringify({ ok: true, data: value, request_id: "req_detail" }), {
    status: 200,
    headers: { "content-type": "application/json" }
  });
}

function client(transport: typeof globalThis.fetch, language: Language): BuyerApiClient {
  return createBuyerApiClient({
    baseUrl: "http://api.test",
    getLanguage: () => language,
    fetch: transport,
    resilience: { retry: { maxAttempts: 1 } }
  });
}

describe("useOrderFeed", () => {
  it("keeps the ready resource reference across unrelated rerenders", async () => {
    let requests = 0;
    const transport: typeof globalThis.fetch = async () => {
      requests += 1;
      return page([order("ord_stable")], null);
    };
    const buyerClient = client(transport, "zh");
    const { result, rerender } = renderHook(
      ({ version }) => {
        void version;
        return useOrderFeed(buyerClient, "zh");
      },
      { initialProps: { version: 0 } }
    );
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    const resource = result.current.resource;

    rerender({ version: 1 });

    expect(result.current.resource).toBe(resource);
    expect(requests).toBe(1);
  });

  it("does not request orders until enabled", async () => {
    let requests = 0;
    const transport: typeof globalThis.fetch = async () => {
      requests += 1;
      return page([order("ord_1")], null);
    };
    const buyerClient = client(transport, "zh");
    const { result, rerender } = renderHook(
      ({ enabled }) => useOrderFeed(buyerClient, "zh", enabled),
      { initialProps: { enabled: false } }
    );
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(requests).toBe(0);
    rerender({ enabled: true });
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    expect(requests).toBe(1);
  });

  it("drops appended rows before reloading after leaving and returning", async () => {
    let firstPage = 0;
    const transport: typeof globalThis.fetch = async (input) => {
      const url = new URL(new Request(input).url);
      if (url.searchParams.has("cursor")) return page([order("old_2")], null);
      firstPage += 1;
      return page([order(firstPage === 1 ? "old_1" : "new_1")], firstPage === 1 ? "next" : null);
    };
    const buyerClient = client(transport, "zh");
    const { result, rerender } = renderHook(
      ({ enabled }) => useOrderFeed(buyerClient, "zh", enabled),
      { initialProps: { enabled: true } }
    );
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.resource.status === "ready" && result.current.resource.value.length).toBe(2));
    rerender({ enabled: false });
    rerender({ enabled: true });
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    expect(result.current.resource.status === "ready" ? result.current.resource.value.map((item) => item.publicId) : []).toEqual(["new_1"]);
  });

  it("deduplicates appended orders and keeps the cursor for retry", async () => {
    const requests: string[] = [];
    const transport: typeof globalThis.fetch = async (input, init) => {
      const request = new Request(input, init);
      requests.push(request.url);
      return request.url.includes("cursor=next")
        ? page([order("ord_1"), order("ord_2")], null)
        : page([order("ord_1")], "next");
    };
    const buyerClient = client(transport, "zh");
    const { result } = renderHook(() => useOrderFeed(buyerClient, "zh"));
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.loadingMore).toBe(false));
    expect(result.current.resource.status === "ready" ? result.current.resource.value.map((item) => item.publicId) : []).toEqual(["ord_1", "ord_2"]);
    expect(requests.some((url) => url.includes("cursor=next"))).toBe(true);
  });

  it("clears an in-flight continuation when language changes", async () => {
    let language: Language = "zh";
    let resolveMore!: (response: Response) => void;
    const more = new Promise<Response>((resolve) => { resolveMore = resolve; });
    const transport: typeof globalThis.fetch = async (input, init) => {
      const request = new Request(input, init);
      if (request.url.includes("cursor=next")) return more;
      return page([order(language === "zh" ? "zh_1" : "en_1")], "next");
    };
    const { result, rerender } = renderHook(
      ({ lang }) => useOrderFeed(client(transport, lang), lang),
      { initialProps: { lang: "zh" as Language } }
    );
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    act(() => result.current.loadMore());
    language = "en";
    rerender({ lang: "en" });
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    resolveMore(page([order("stale")], null));
    await new Promise((resume) => setTimeout(resume, 0));
    expect(result.current.resource.status === "ready" ? result.current.resource.value.map((item) => item.publicId) : []).toEqual(["en_1"]);
  });

  it("retries a failed load-more request with the same cursor", async () => {
    let attempts = 0;
    const transport: typeof globalThis.fetch = async (input, init) => {
      const request = new Request(input, init);
      if (!request.url.includes("cursor=next")) return page([order("ord_1")], "next");
      attempts += 1;
      return attempts === 1
        ? new Response(JSON.stringify({ ok: false, error: { code: 1005, message: { zh: "错误", en: "Error", km: "កំហុស" } }, request_id: "req_test" }), { status: 503, headers: { "content-type": "application/json" } })
        : page([order("ord_2")], null);
    };
    const buyerClient = client(transport, "zh");
    const { result } = renderHook(() => useOrderFeed(buyerClient, "zh"));
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));

    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.moreMessageKey).toBe("error.1005"));
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.loadingMore).toBe(false));
    expect(attempts).toBe(2);
    expect(result.current.resource.status === "ready" ? result.current.resource.value.map((item) => item.publicId) : []).toEqual(["ord_1", "ord_2"]);
  });
});

describe("useOrderDetail", () => {
  it("retries a failed detail request", async () => {
    let attempts = 0;
    const transport: typeof globalThis.fetch = async () => {
      attempts += 1;
      return attempts === 1
        ? new Response(JSON.stringify({ ok: false, error: { code: 1005, message: { zh: "错误", en: "Error", km: "កំហុស" } }, request_id: "req_detail" }), { status: 503, headers: { "content-type": "application/json" } })
        : detail(order("ord_detail"));
    };
    const buyerClient = client(transport, "zh");
    const { result } = renderHook(() => useOrderDetail(buyerClient, "ord_detail", "zh"));
    await waitFor(() => expect(result.current.resource.status).toBe("error"));
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    expect(attempts).toBe(2);
  });

  it("reloads the open detail when the language changes", async () => {
    let language: Language = "zh";
    const languages: string[] = [];
    const transport: typeof globalThis.fetch = async (input, init) => {
      const request = new Request(input, init);
      languages.push(request.headers.get("Accept-Language") ?? "");
      return detail(order("ord_detail"));
    };
    const buyerClient = createBuyerApiClient({
      baseUrl: "http://api.test",
      getLanguage: () => language,
      fetch: transport,
      resilience: { retry: { maxAttempts: 1 } }
    });
    const { result, rerender } = renderHook(
      ({ lang }) => useOrderDetail(buyerClient, "ord_detail", lang),
      { initialProps: { lang: "zh" as Language } }
    );
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    language = "km";
    rerender({ lang: "km" });
    await waitFor(() => expect(languages).toEqual(["zh", "km"]));
  });

  it("does not render the previous order while switching detail ids", async () => {
    const transport: typeof globalThis.fetch = async (input, init) => {
      const request = new Request(input, init);
      return detail(order(new URL(request.url).pathname.endsWith("ord_b") ? "ord_b" : "ord_a"));
    };
    const buyerClient = client(transport, "zh");
    const { result, rerender } = renderHook(
      ({ publicId }) => useOrderDetail(buyerClient, publicId, "zh"),
      { initialProps: { publicId: "ord_a" } }
    );
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    rerender({ publicId: "ord_b" });
    expect(result.current.resource.status).toBe("loading");
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));
    expect(result.current.resource.status === "ready" ? result.current.resource.value.publicId : "").toBe("ord_b");
  });
});
