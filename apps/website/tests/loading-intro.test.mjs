import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { initLoadingIntro } from "../src/scripts/loading-intro.mjs";

function createEnvironment({ reduced = false, seen = false, storageThrows = false } = {}) {
  let now = 0;
  let nextId = 1;
  const timers = new Map();
  const listeners = new Map();
  const classes = new Set();
  const overlay = {
    classList: { add: (name) => classes.add(name), contains: (name) => classes.has(name) },
    removed: false,
    remove() { this.removed = true; }
  };
  const storage = new Map(seen ? [["drlife-loading-intro-seen", "1"]] : []);
  const setTimeout = (callback, delay) => {
    const id = nextId++;
    timers.set(id, { callback, at: now + delay });
    return id;
  };
  const clearTimeout = (id) => timers.delete(id);
  const advanceTo = (target) => {
    while (true) {
      const due = [...timers.entries()].filter(([, timer]) => timer.at <= target).sort((a, b) => a[1].at - b[1].at)[0];
      if (!due) break;
      const [id, timer] = due;
      timers.delete(id);
      now = timer.at;
      timer.callback();
    }
    now = target;
  };
  const windowObject = {
    addEventListener: (name, callback) => listeners.set(name, callback),
    clearTimeout,
    matchMedia: () => ({ matches: reduced }),
    requestAnimationFrame: (callback) => setTimeout(callback, 0),
    sessionStorage: {
      getItem(key) { if (storageThrows) throw new Error("blocked"); return storage.get(key) ?? null; },
      setItem(key, value) { if (storageThrows) throw new Error("blocked"); storage.set(key, value); }
    },
    setTimeout
  };
  const documentObject = {
    readyState: "loading",
    body: { appended: [], append(node) { this.appended.push(node); } },
    getElementById: () => ({ content: { firstElementChild: { cloneNode: () => overlay } } })
  };

  return { advanceTo, documentObject, fireLoad: () => listeners.get("load")?.(), overlay, storage, windowObject };
}

test("loading intro uses the shared text wordmark without office branding or raster logos", () => {
  const source = readFileSync(new URL("../src/components/LoadingIntro.astro", import.meta.url), "utf8");
  assert.match(source, /<BrandWordmark class="site-loader__brand"/);
  assert.doesNotMatch(source, /logo\.png|智序工场|NiuMa/);
});

test("fast pages never show the loading intro", () => {
  const env = createEnvironment();
  initLoadingIntro(env.windowObject, env.documentObject);
  env.documentObject.readyState = "complete";
  env.fireLoad();
  env.advanceTo(300);
  assert.equal(env.documentObject.body.appended.length, 0);
});

test("slow pages show after 300ms and remove the intro within 1.2s", () => {
  const env = createEnvironment();
  initLoadingIntro(env.windowObject, env.documentObject);
  env.advanceTo(299);
  assert.equal(env.documentObject.body.appended.length, 0);
  env.advanceTo(300);
  assert.equal(env.documentObject.body.appended.length, 1);
  assert.equal(env.storage.get("drlife-loading-intro-seen"), "1");
  env.advanceTo(1500);
  assert.equal(env.overlay.removed, true);
});

test("load completion during the fade cannot leave a hidden overlay behind", () => {
  const env = createEnvironment();
  initLoadingIntro(env.windowObject, env.documentObject);
  env.advanceTo(1320);
  env.fireLoad();
  env.advanceTo(1500);
  assert.equal(env.overlay.removed, true);
});

test("session and reduced-motion preferences suppress the loading intro", () => {
  for (const options of [{ seen: true }, { reduced: true }]) {
    const env = createEnvironment(options);
    initLoadingIntro(env.windowObject, env.documentObject);
    env.advanceTo(2000);
    assert.equal(env.documentObject.body.appended.length, 0);
  }
});

test("blocked session storage never breaks or traps the page", () => {
  const env = createEnvironment({ storageThrows: true });
  assert.doesNotThrow(() => initLoadingIntro(env.windowObject, env.documentObject));
  env.advanceTo(1500);
  assert.equal(env.overlay.removed, true);
});
