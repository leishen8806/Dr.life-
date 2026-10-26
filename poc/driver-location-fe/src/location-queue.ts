import type {
  FlushReport,
  LocationPoint,
  PositionSample,
  QueueStorage,
  Uploader
} from "./types";

export const QUEUE_STORAGE_KEY = "drlife.driver.location.queue.v1";
const MAX_QUEUED_POINTS = 1_000;

export class MemoryQueueStorage implements QueueStorage {
  private readonly values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }
}

export interface LocationQueueOptions {
  storage: QueueStorage;
  uploader: Uploader;
  now?: () => Date;
  createId?: () => string;
  storageKey?: string;
}

export class LocationQueue {
  private readonly storage: QueueStorage;
  private readonly uploader: Uploader;
  private readonly now: () => Date;
  private readonly createId: () => string;
  private readonly storageKey: string;
  private points: LocationPoint[];
  private flushPromise: Promise<FlushReport> | null = null;

  constructor(options: LocationQueueOptions) {
    this.storage = options.storage;
    this.uploader = options.uploader;
    this.now = options.now ?? (() => new Date());
    this.createId = options.createId ?? (() => crypto.randomUUID());
    this.storageKey = options.storageKey ?? QUEUE_STORAGE_KEY;
    this.points = this.read();
  }

  pending(): LocationPoint[] {
    return this.points.map((point) => ({ ...point }));
  }

  enqueue(input: PositionSample & { id?: string; capturedAt?: string }): LocationPoint {
    const id = input.id ?? this.createId();
    const existing = this.points.find((point) => point.id === id);
    if (existing) return { ...existing };

    const point: LocationPoint = {
      id,
      lat: input.lat,
      lng: input.lng,
      accuracyM: input.accuracyM,
      capturedAt: input.capturedAt ?? this.now().toISOString()
    };
    this.points = [...this.points, point].slice(-MAX_QUEUED_POINTS);
    this.persist();
    return { ...point };
  }

  flush(): Promise<FlushReport> {
    if (this.flushPromise) return this.flushPromise;
    this.flushPromise = this.runFlush().finally(() => {
      this.flushPromise = null;
    });
    return this.flushPromise;
  }

  private async runFlush(): Promise<FlushReport> {
    const snapshot = this.points.slice();
    let uploaded = 0;

    for (const point of snapshot) {
      try {
        await this.uploader.upload(point);
      } catch {
        break;
      }
      this.points = this.points.filter((queued) => queued.id !== point.id);
      this.persist();
      uploaded += 1;
    }

    return {
      attempted: snapshot.length,
      uploaded,
      pending: this.points.length
    };
  }

  private read(): LocationPoint[] {
    try {
      const raw = this.storage.getItem(this.storageKey);
      if (!raw) return [];
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) return [];
      return parsed.filter(isLocationPoint).slice(-MAX_QUEUED_POINTS);
    } catch {
      return [];
    }
  }

  private persist(): void {
    try {
      this.storage.setItem(this.storageKey, JSON.stringify(this.points));
    } catch {
      // A restricted browser can reject storage writes; the in-memory queue still works.
    }
  }
}

function isLocationPoint(value: unknown): value is LocationPoint {
  if (!value || typeof value !== "object") return false;
  const point = value as Partial<LocationPoint>;
  return (
    typeof point.id === "string" &&
    point.id.length > 0 &&
    typeof point.lat === "number" &&
    Number.isFinite(point.lat) &&
    typeof point.lng === "number" &&
    Number.isFinite(point.lng) &&
    typeof point.accuracyM === "number" &&
    Number.isFinite(point.accuracyM) &&
    typeof point.capturedAt === "string" &&
    !Number.isNaN(Date.parse(point.capturedAt))
  );
}
