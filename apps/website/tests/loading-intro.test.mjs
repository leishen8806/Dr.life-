import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import vm from "node:vm";
import { initLoadingIntro } from "../src/scripts/loading-intro.mjs";

const componentSource = readFileSync(new URL("../src/components/LoadingIntro.astro", import.meta.url), "utf8");
const gateSource = readFileSync(new URL("../public/scripts/intro-gate.js", import.meta.url), "utf8");
const layoutSource = readFileSync(new URL("../src/components/Layout.astro", import.meta.url), "utf8");

function classList(initial = []) {
  const classes = new Set(initial);
  return {
    add: (name) => classes.add(name),
    contains: (name) => classes.has(name),
    remove: (name) => classes.delete(name)
  };
}

function runGate({ reduced = false, seen = false, storageThrows = false } = {}) {
  const htmlClasses = classList();
  const storage = new Map(seen ? [["drlife-loading-intro-seen", "1"]] : []);
  const windowObject = {
    matchMedia: () => ({ matches: reduced }),
    sessionStorage: {
      getItem(key) { if (storageThrows) throw new Error("blocked"); return storage.get(key) ?? null; },
      setItem(key, value) { if (storageThrows) throw new Error("blocked"); storage.set(key, value); }
    }
  };
  vm.runInNewContext(gateSource, { document: { documentElement: { classList: htmlClasses } }, window: windowObject });
  return { htmlClasses, storage };
}

function createEnvironment({ intro = true, nowAtStart = 0 } = {}) {
  let now = nowAtStart;
  let nextId = 1;
  const timers = new Map();
  const listeners = new Map();
  const htmlClasses = classList(intro ? ["intro"] : []);
  const overlayClasses = classList();
  const overlay = {
    classList: overlayClasses,
    removed: false,
    remove() { this.removed = true; }
  };
  const setTimeout = (callback, delay) => {
    const id = nextId++;
    timers.set(id, { callback, at: now + delay });
    return id;
  };
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
    clearTimeout: (id) => timers.delete(id),
    performance: { now: () => now },
    setTimeout
  };
  const documentObject = {
    documentElement: { classList: htmlClasses },
    fonts: { ready: new Promise(() => {}) },
    getElementById: () => overlay,
    readyState: "loading"
  };
  return { advanceTo, documentObject, fireDomReady: () => listeners.get("DOMContentLoaded")?.(), htmlClasses, overlay, windowObject };
}

test("loading intro uses the shared text wordmark without office branding or raster logos", () => {
  assert.match(componentSource, /<BrandWordmark class="site-loader__brand"/);
  assert.doesNotMatch(componentSource, /logo\.png|智序工场|NiuMa/);
});

test("only html.intro makes the server-rendered overlay visible", () => {
  assert.match(componentSource, /\.site-loader\s*\{[\s\S]*?display:\s*none;/);
  assert.match(componentSource, /:global\(html\.intro\) \.site-loader\s*\{\s*display:\s*grid;/);
});

test("the gate is a synchronous classic script in the document head", () => {
  assert.match(layoutSource, /<head>[\s\S]*?<script is:inline src="\/scripts\/intro-gate\.js"><\/script>/);
  assert.doesNotMatch(layoutSource, /intro-gate\.js[^>]*(?:defer|type="module")/);
});

test("the synchronous gate enables only the first motion-enabled visit", () => {
  const first = runGate();
  assert.equal(first.htmlClasses.contains("intro"), true);
  assert.equal(first.storage.get("drlife-loading-intro-seen"), "1");
  assert.equal(runGate({ seen: true }).htmlClasses.contains("intro"), false);
  assert.equal(runGate({ reduced: true }).htmlClasses.contains("intro"), false);
  assert.doesNotThrow(() => runGate({ storageThrows: true }));
});

test("a missing intro class removes the hidden overlay immediately", () => {
  const env = createEnvironment({ intro: false });
  initLoadingIntro(env.windowObject, env.documentObject);
  assert.equal(env.overlay.removed, true);
});

test("the overlay is always removed by 1.2 seconds from navigation start", () => {
  const env = createEnvironment({ nowAtStart: 125 });
  initLoadingIntro(env.windowObject, env.documentObject);
  env.advanceTo(1199);
  assert.equal(env.overlay.removed, false);
  env.advanceTo(1200);
  assert.equal(env.overlay.removed, true);
  assert.equal(env.htmlClasses.contains("intro"), false);
});
