import type { Product, ProductPage } from "./types";

/**
 * A product list as far as the buyer has pulled it in: the pages loaded so far
 * plus the total the server reported, which is what the count label shows.
 */
export interface ProductFeed {
  items: Product[];
  /**
   * The pages pulled in so far, counted here rather than read back from the
   * server's echoed `page`: a server that keeps echoing page 1 would otherwise
   * have the next page asked for over and over.
   */
  page: number;
  total: number;
  hasMore: boolean;
}

/**
 * A server that reports a total it cannot deliver would otherwise leave the
 * control on screen forever, so a page that brought no new row ends the feed.
 */
function feed(
  items: Product[],
  page: number,
  total: number,
  added: number
): ProductFeed {
  return { items, page, total, hasMore: added > 0 && items.length < total };
}

export function firstFeed(page: ProductPage): ProductFeed {
  return feed([...page.items], 1, page.total, page.items.length);
}

/** The page the feed needs next, or nothing when it has reached the end. */
export function nextPage(current: ProductFeed): number | undefined {
  return current.hasMore ? current.page + 1 : undefined;
}

/** Pages can overlap while the catalogue changes, so ids are not repeated. */
export function appendFeed(current: ProductFeed, page: ProductPage): ProductFeed {
  const seen = new Set(current.items.map((item) => item.public_id));
  const added = page.items.filter((item) => !seen.has(item.public_id));
  return feed(
    [...current.items, ...added],
    current.page + 1,
    page.total,
    added.length
  );
}
