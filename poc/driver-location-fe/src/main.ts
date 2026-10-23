import "./styles.css";
import { isLanguage, text, type CopyKey, type Language } from "./copy";
import { createHttpUploader } from "./http-uploader";
import { LocationQueue, MemoryQueueStorage } from "./location-queue";
import { LocationSampler, type SamplerEvent } from "./location-sampler";
import type {
  FlushReport,
  LocationPoint,
  PositionSample,
  QueueStorage,
  Uploader
} from "./types";

const LANGUAGE_KEY = "drlife.driver.location.lang";
const UPLOADED_KEY = "drlife.driver.location.uploaded.v1";
const params = new URLSearchParams(location.search);
const simulated = params.get("fakeLocation") === "1";
const intervalMs = clamp(
  Number(params.get("intervalMs") ?? 5_000),
  1_000,
  60_000
);
const endpoint = params.get("endpoint") ?? "/v1/driver/locations";

const languageOrder: Language[] = ["zh", "en", "km"];
const languageNameKeys: Record<Language, CopyKey> = {
  zh: "zhName",
  en: "enName",
  km: "kmName"
};

const elements = {
  language: byId<HTMLDivElement>("language"),
  network: byId<HTMLSpanElement>("network"),
  mode: byId<HTMLSpanElement>("mode"),
  pending: byId<HTMLElement>("pending"),
  uploaded: byId<HTMLElement>("uploaded"),
  lastCaptured: byId<HTMLElement>("last-captured"),
  lastError: byId<HTMLElement>("last-error"),
  interval: byId<HTMLElement>("interval"),
  queue: byId<HTMLUListElement>("queue"),
  queueCount: byId<HTMLElement>("queue-count"),
  retry: byId<HTMLButtonElement>("retry"),
  version: byId<HTMLElement>("version"),
  live: byId<HTMLElement>("live")
};

let language = readLanguage();
let uploadedTotal = readUploadedTotal();
let lastCaptured: LocationPoint | null = null;
let lastError: string | null = null;
let flushing = false;
let simulatedSequence = 0;

const httpUploader = createHttpUploader(endpoint);
const uploader: Uploader = {
  async upload(point) {
    try {
      await httpUploader.upload(point);
    } catch (error) {
      lastError = text(language, "uploadFailed");
      console.error("[location-upload]", error);
      render();
      throw error;
    }
  }
};

const queue = new LocationQueue({
  storage: resolveStorage(),
  uploader
});
const pendingAtLoad = queue.pending();
lastCaptured = pendingAtLoad.at(-1) ?? null;

const sampler = new LocationSampler({
  queue,
  intervalMs,
  getPosition,
  onEvent: handleSamplerEvent
});

renderStatic();
render();
void sampler.tick();
sampler.start();

elements.retry.addEventListener("click", () => {
  void flushNow();
});
window.addEventListener("online", () => {
  lastError = null;
  render();
  void flushNow();
});
window.addEventListener("offline", () => {
  lastError = text(language, "offlineHint");
  render();
});
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") void flushNow();
});

if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("/sw.js").catch((error: unknown) => {
      console.error("[service-worker]", error);
    });
  });
}

function handleSamplerEvent(event: SamplerEvent): void {
  switch (event.type) {
    case "captured":
      lastCaptured = event.point;
      lastError = null;
      break;
    case "flush":
      applyFlushReport(event.report);
      break;
    case "capture-error":
      lastError = simulated
        ? text(language, "permissionDenied")
        : navigator.geolocation
          ? text(language, "permissionDenied")
          : text(language, "geolocationUnavailable");
      console.error("[location-capture]", event.error);
      break;
    case "flush-error":
      lastError = text(language, "uploadFailed");
      console.error("[location-flush]", event.error);
      break;
  }
  render();
}

function applyFlushReport(report: FlushReport): void {
  if (report.uploaded > 0) {
    uploadedTotal += report.uploaded;
    persistUploadedTotal();
    lastError = null;
    elements.live.textContent = text(language, "flushDone", {
      count: report.uploaded
    });
  }
  if (report.pending === 0) lastError = null;
}

async function flushNow(): Promise<void> {
  if (flushing) return;
  if (!navigator.onLine) {
    lastError = text(language, "offlineHint");
    render();
    return;
  }

  flushing = true;
  render();
  try {
    const report = await queue.flush();
    applyFlushReport(report);
  } catch (error) {
    lastError = text(language, "uploadFailed");
    console.error("[location-flush]", error);
  } finally {
    flushing = false;
    render();
  }
}

async function getPosition(): Promise<PositionSample> {
  if (simulated) {
    simulatedSequence += 1;
    return {
      lat: 11.5564 + simulatedSequence * 0.00001,
      lng: 104.9282,
      accuracyM: 5
    };
  }
  if (!navigator.geolocation) {
    throw new Error("geolocation unavailable");
  }
  return new Promise((resolve, reject) => {
    navigator.geolocation.getCurrentPosition(
      (position) => {
        resolve({
          lat: position.coords.latitude,
          lng: position.coords.longitude,
          accuracyM: position.coords.accuracy
        });
      },
      (error) => reject(new Error(`geolocation ${error.code}: ${error.message}`)),
      { enableHighAccuracy: true, timeout: 10_000, maximumAge: 0 }
    );
  });
}

function renderStatic(): void {
  document.documentElement.lang = language;
  document.title = `${text(language, "appTitle")} · PoC`;
  for (const node of document.querySelectorAll<HTMLElement>("[data-i18n]")) {
    const key = node.dataset.i18n as CopyKey | undefined;
    if (key) node.textContent = text(language, key);
  }
  elements.language.setAttribute("aria-label", text(language, "languageAria"));
  elements.language.replaceChildren(
    ...languageOrder.map((candidate) => {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = text(language, languageNameKeys[candidate]);
      button.classList.toggle("active", candidate === language);
      button.setAttribute("aria-pressed", String(candidate === language));
      button.addEventListener("click", () => {
        language = candidate;
        persistLanguage();
        renderStatic();
        render();
      });
      return button;
    })
  );
}

function render(): void {
  const online = navigator.onLine;
  const pending = queue.pending();

  elements.network.textContent = text(language, online ? "online" : "offline");
  elements.network.className = `badge ${online ? "online" : "offline"}`;
  elements.mode.textContent = text(
    language,
    simulated ? "modeSimulated" : "modeGps"
  );
  elements.mode.className = `badge ${simulated ? "simulated" : "muted"}`;
  elements.pending.textContent = String(pending.length);
  elements.uploaded.textContent = String(uploadedTotal);
  elements.lastCaptured.textContent = lastCaptured
    ? formatTime(lastCaptured.capturedAt)
    : text(language, "noLocation");
  elements.lastError.textContent = lastError ?? text(language, "noError");
  elements.lastError.classList.toggle("error", Boolean(lastError));
  elements.interval.textContent = text(language, "samplingEvery", {
    seconds: Math.round(intervalMs / 1_000)
  });
  elements.retry.textContent = flushing
    ? text(language, "retryBusy")
    : text(language, "retryNow");
  elements.retry.disabled = flushing || !online;
  elements.version.textContent = __BUILD_ID__;
  elements.queueCount.textContent = String(pending.length);

  const rows = pending
    .slice()
    .reverse()
    .map((point) => renderQueueRow(point));
  if (rows.length === 0) {
    const empty = document.createElement("li");
    empty.className = "empty";
    empty.textContent = text(language, "queueEmpty");
    rows.push(empty);
  }
  elements.queue.replaceChildren(...rows);
}

function renderQueueRow(point: LocationPoint): HTMLLIElement {
  const row = document.createElement("li");
  const head = document.createElement("div");
  head.className = "queue-row";
  const time = document.createElement("strong");
  time.textContent = formatTime(point.capturedAt);
  const coords = document.createElement("span");
  coords.textContent = `${point.lat.toFixed(5)}, ${point.lng.toFixed(5)}`;
  head.append(time, coords);

  const meta = document.createElement("small");
  meta.textContent = `±${Math.round(point.accuracyM)} m · ${point.id.slice(0, 8)}`;
  row.append(head, meta);
  return row;
}

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const locale =
    language === "zh" ? "zh-CN" : language === "km" ? "km-KH" : "en-GB";
  return new Intl.DateTimeFormat(locale, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false
  }).format(date);
}

function readLanguage(): Language {
  try {
    const value = localStorage.getItem(LANGUAGE_KEY);
    return isLanguage(value) ? value : "zh";
  } catch {
    return "zh";
  }
}

function persistLanguage(): void {
  try {
    localStorage.setItem(LANGUAGE_KEY, language);
  } catch {
    // Language persistence is optional.
  }
}

function readUploadedTotal(): number {
  try {
    const value = Number(localStorage.getItem(UPLOADED_KEY));
    return Number.isFinite(value) && value > 0 ? value : 0;
  } catch {
    return 0;
  }
}

function persistUploadedTotal(): void {
  try {
    localStorage.setItem(UPLOADED_KEY, String(uploadedTotal));
  } catch {
    // Session-only counter is acceptable when storage is unavailable.
  }
}

function resolveStorage(): QueueStorage {
  try {
    localStorage.getItem(LANGUAGE_KEY);
    return localStorage;
  } catch {
    return new MemoryQueueStorage();
  }
}

function byId<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing #${id}`);
  return element as T;
}

function clamp(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return minimum;
  return Math.min(maximum, Math.max(minimum, value));
}
