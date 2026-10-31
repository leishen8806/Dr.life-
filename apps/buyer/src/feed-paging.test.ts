import { describe, expect, it } from "vitest";
import { appendFeed, firstFeed, nextPage } from "./feed";
import type { Product, ProductPage } from "./types";

function product(public_id: string): Product {
  return {
    public_id,
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

const catalogue = ["a", "b", "c", "d", "e"].map(product);
const pageSize = 2;

/** The paging behaviours a deployed server can show the buyer app. */
type Behaviour = "increments" | "stuck-on-page-one" | "empty-but-more";

const behaviours: Behaviour[] = [
  "increments",
  "stuck-on-page-one",
  "empty-but-more"
];

function slice(page: number): Product[] {
  return catalogue.slice((page - 1) * pageSize, page * pageSize);
}

function serve(behaviour: Behaviour, page: number): ProductPage {
  switch (behaviour) {
    case "increments":
      return { items: slice(page), page, page_size: pageSize, total: catalogue.length };
    /** Echoes page 1 whatever was asked for, so the echo cannot drive paging. */
    case "stuck-on-page-one":
      return { items: slice(1), page: 1, page_size: pageSize, total: catalogue.length };
    /** Reports a total it will not deliver: every page after the first is empty. */
    case "empty-but-more":
      return { items: page === 1 ? slice(1) : [], page, page_size: pageSize, total: 57 };
  }
}

/** A runaway feed must fail the test rather than hang it. */
const requestCap = 8;

/**
 * Replays what `useProductFeed` does to page one category: the first page, then
 * `nextPage`/`appendFeed` for as long as the feed still offers more. `requested`
 * is the sequence of `page` values that reached the API.
 */
function drain(behaviour: Behaviour) {
  const requested = [1];
  let feed = firstFeed(serve(behaviour, 1));
  while (requested.length <= requestCap) {
    const page = nextPage(feed);
    if (page === undefined) break;
    requested.push(page);
    feed = appendFeed(feed, serve(behaviour, page));
  }
  return { feed, requested };
}

describe("category paging (the useProductFeed glue)", () => {
  it("walks a server that increments the page to the end of the list", () => {
    const { feed, requested } = drain("increments");

    expect(requested).toEqual([1, 2, 3]);
    expect(feed.items.map((item) => item.public_id)).toEqual([
      "a",
      "b",
      "c",
      "d",
      "e"
    ]);
    expect(feed.hasMore).toBe(false);
  });

  /**
   * The failure this guards: paging used to take the next page number from the
   * server's echoed `page`, so a server stuck on page 1 was asked for page 2 for
   * ever. Every answer deduplicated to nothing, no row was ever added, and the
   * control stayed on screen because the total was still out of reach.
   */
  it("stops instead of re-asking when the server echoes page 1 for ever", () => {
    const { feed, requested } = drain("stuck-on-page-one");

    expect(requested).toEqual([1, 2]);
    expect(feed.hasMore).toBe(false);
    expect(feed.items.map((item) => item.public_id)).toEqual(["a", "b"]);
  });

  it("counts the pages itself, so the number it asks for ignores the echo", () => {
    const first = firstFeed(serve("stuck-on-page-one", 1));
    expect(nextPage(first)).toBe(2);

    const second = appendFeed(first, serve("stuck-on-page-one", 2));
    expect(second.page).toBe(2);
  });

  it("ends the feed when the server reports a total it will not deliver", () => {
    const { feed, requested } = drain("empty-but-more");

    expect(requested).toEqual([1, 2]);
    expect(feed.hasMore).toBe(false);
    expect(feed.total).toBe(57);
    expect(feed.items.map((item) => item.public_id)).toEqual(["a", "b"]);
  });

  /**
   * The first page is page 1 because that is what the client asked for, not
   * because the server said so. Reading the echo here would resume from the
   * wrong place and skip whole pages without a trace, and every behaviour
   * above happens to echo page 1 for the first page, so none of them can tell
   * the two apart. Hence a deliberately wrong echo.
   */
  it("counts the first page as page 1 even when the server echoes another number", () => {
    const first = firstFeed({
      items: slice(1),
      page: 7,
      page_size: pageSize,
      total: catalogue.length
    });

    expect(first.page).toBe(1);
    expect(nextPage(first)).toBe(2);
  });

  it("asks for every page at most once, whatever the server answers", () => {
    for (const behaviour of behaviours) {
      const { requested } = drain(behaviour);

      expect(new Set(requested).size, behaviour).toBe(requested.length);
      expect([...requested].sort((a, b) => a - b), behaviour).toEqual(requested);
      expect(requested.length, behaviour).toBeLessThanOrEqual(requestCap);
    }
  });

  it("never offers more without having added a row for it", () => {
    for (const behaviour of behaviours) {
      const { feed } = drain(behaviour);

      expect(feed.hasMore, behaviour).toBe(false);
      expect(feed.items.length, behaviour).toBeGreaterThan(0);
    }
  });
});
