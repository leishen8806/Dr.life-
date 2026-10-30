import type { Product, ProductPage } from "./types";

/**
 * A product list as far as the buyer has pulled it in: the pages loaded so far
 * plus the total the server reported, which is what the count label shows.
 */
export interface ProductFeed {
  items: Product[];
  page: number;
  total: number;
  hasMore: boolean;
}

/**
 * A server that reports a total it cannot deliver would otherwise leave the
 * control on screen forever, so an empty page also ends the feed.
 */
function feed(items: Product[], page: ProductPage): ProductFeed {
  return {
    items,
    page: page.page,
    total: page.total,
    hasMore: items.length < page.total && page.items.length > 0
  };
}

export function firstFeed(page: ProductPage): ProductFeed {
  return feed([...page.items], page);
}

/** Pages can overlap while the catalogue changes, so ids are not repeated. */
export function appendFeed(current: ProductFeed, page: ProductPage): ProductFeed {
  const seen = new Set(current.items.map((item) => item.public_id));
  const added = page.items.filter((item) => !seen.has(item.public_id));
  return feed([...current.items, ...added], page);
}
