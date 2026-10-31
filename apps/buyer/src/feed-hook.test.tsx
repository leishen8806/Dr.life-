// @vitest-environment jsdom
/**
 * The two guards in `useProductFeed` only run on a mounted hook: the view
 * disables the control while a page is in flight, so the re-entry guard is
 * unreachable from the rendered markup, and the stale guard needs the loader to
 * change under a live component. Hence the DOM environment for this file alone.
 */
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Language } from "@drlife/i18n";
import { type BuyerApiClient, createBuyerApiClient } from "./api";
import { useProductFeed } from "./app";
import type { ProductFeed } from "./feed";
import type { Resource } from "./resource";
import type { Product } from "./types";

afterEach(cleanup);

function product(publicId: string): Product {
  return {
    public_id: publicId,
    category_code: "c4",
    name: { zh: "瓷砖", en: "Tile", km: "ក្បឿង" },
    unit: "m2",
    origin: "cn",
    status: "on",
    source: "manual",
    tiers: [{ min_qty: "1", unit_price_e4: 95_000 }],
    skus: [],
    updated_at: "2026-10-29T00:00:00Z"
  };
}

function productPage(items: Product[], page: number, total: number): Response {
  return new Response(
    JSON.stringify({
      ok: true,
      data: { items, page, page_size: 20, total },
      request_id: "req_test"
    }),
    { status: 200, headers: { "content-type": "application/json" } }
  );
}

/** A request the test resolves by hand, which is what "in flight" means here. */
function held() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function recordingFetch(answer: (url: URL) => Response | Promise<Response>) {
  const urls: string[] = [];
  const transport: typeof globalThis.fetch = async (input, init) => {
    const { url } = new Request(input, init);
    urls.push(url);
    return answer(new URL(url));
  };
  return { urls, transport };
}

/**
 * `requests` records what the hook asked the client for. Middleware runs per
 * call, above the client's coalescing of identical in-flight GETs: counting at
 * the transport would pass even with the re-entry guard gone.
 */
function client(
  transport: typeof globalThis.fetch,
  language: Language = "zh",
  requests?: string[]
): BuyerApiClient {
  const api = createBuyerApiClient({
    baseUrl: "http://api.test",
    getLanguage: () => language,
    fetch: transport,
    resilience: { retry: { maxAttempts: 1 } }
  });
  if (requests)
    api.use({
      onRequest: ({ request }) => {
        requests.push(request.url);
        return request;
      }
    });
  return api;
}

function ids(resource: Resource<ProductFeed>): string[] {
  return resource.status === "ready"
    ? resource.value.items.map((item) => item.public_id)
    : [];
}

/** Lets every pending reply and the state update it causes run to completion. */
async function settle(): Promise<void> {
  await act(async () => {
    await new Promise((resume) => setTimeout(resume, 0));
  });
}

describe("useProductFeed", () => {
  /** Guard one: a second trigger while a page loads must not ask again. */
  it("asks for the next page once while that page is in flight", async () => {
    const nextPage = held();
    const { urls, transport } = recordingFetch((url) =>
      url.searchParams.get("page") === "2"
        ? nextPage.promise
        : productPage([product("prd_1")], 1, 5)
    );
    const requests: string[] = [];
    const api = client(transport, "zh", requests);

    const { result } = renderHook(() => useProductFeed(api, "c4"));
    await waitFor(() => expect(result.current.resource.status).toBe("ready"));

    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.loadingMore).toBe(true));
    act(() => result.current.loadMore());
    await settle();

    expect(requests.filter((url) => url.includes("page=2"))).toHaveLength(1);
    expect(urls).toEqual([
      "http://api.test/v1/public/products?page_size=20&category=c4",
      "http://api.test/v1/public/products?page=2&page_size=20&category=c4"
    ]);
    expect(result.current.loadingMore).toBe(true);
  });

  /**
   * Guard two: a language switch hands the hook a new client, so the page that
   * was in flight belongs to a list that is no longer on screen.
   */
  it("drops a page that the previous loader had asked for", async () => {
    const nextPage = held();
    let firstPages = 0;
    const { urls, transport } = recordingFetch((url) => {
      if (url.searchParams.get("page") === "2") return nextPage.promise;
      firstPages += 1;
      return productPage([product(`prd_first_${firstPages}`)], 1, 5);
    });
    const { result, rerender } = renderHook(
      ({ api }) => useProductFeed(api, "c4"),
      { initialProps: { api: client(transport, "zh") } }
    );
    // The waits below settle on requests, not on rows, so that the rows are
    // asserted once: after the page of the abandoned list has come back.
    await waitFor(() => expect(urls).toHaveLength(1));

    act(() => result.current.loadMore());
    await waitFor(() => expect(urls).toHaveLength(2));

    rerender({ api: client(transport, "km") });
    await waitFor(() => expect(urls).toHaveLength(3));
    await settle();

    nextPage.resolve(productPage([product("prd_late")], 2, 5));
    await settle();

    expect(ids(result.current.resource)).toEqual(["prd_first_2"]);
    expect(result.current.loadingMore).toBe(false);
  });
});
