const CACHE_NAME = "drlife-driver-location-__BUILD_ID__";
const SHELL_URL = "/index.html";
const PRECACHE_URLS = __PRECACHE_URLS__;

self.addEventListener("install", (event) => {
  event.waitUntil(precache().then(() => self.skipWaiting()));
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
        )
      )
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith("/__mock/") || url.pathname.startsWith("/v1/")) {
    return;
  }

  event.respondWith(handleRequest(request));
});

async function handleRequest(request) {
  const cache = await caches.open(CACHE_NAME);

  if (request.mode === "navigate") {
    try {
      const response = await fetch(request);
      if (response.ok) {
        await cache.put(SHELL_URL, response.clone());
      }
      return response;
    } catch {
      const cached = await matchCached(cache, SHELL_URL);
      return cached || offlineDocument();
    }
  }

  const cached = await matchCached(cache, request);
  if (cached) return cached;

  try {
    const response = await fetch(request);
    if (response.ok && response.type === "basic") {
      await cache.put(request, response.clone());
    }
    return response;
  } catch {
    return new Response("", {
      status: 504,
      statusText: "Offline asset unavailable"
    });
  }
}

async function matchCached(cache, target) {
  const direct = await cache.match(target, { ignoreVary: true });
  if (direct) return direct;
  const pathname =
    typeof target === "string" ? target : new URL(target.url).pathname;
  return (await cache.match(pathname, { ignoreVary: true })) || undefined;
}

function offlineDocument() {
  const html = [
    "<!doctype html><html><head><meta charset=utf-8><title>Offline</title></head>",
    "<body><p>Dr.life+ PoC shell is not cached yet. Reconnect once and reload.</p></body></html>"
  ].join("");
  return new Response(html, {
    status: 503,
    headers: { "Content-Type": "text/html; charset=utf-8" }
  });
}

async function precache() {
  const cache = await caches.open(CACHE_NAME);
  await Promise.all(
    PRECACHE_URLS.map(async (url) => {
      try {
        const response = await fetch(url, { cache: "no-store" });
        if (response.ok) {
          await cache.put(new Request(url), response.clone());
        }
      } catch {
        // A missing optional asset must not block the offline shell.
      }
    })
  );
}
