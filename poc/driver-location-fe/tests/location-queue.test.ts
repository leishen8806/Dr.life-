import { afterEach, describe, expect, it, vi } from "vitest";
import { createHttpUploader } from "../src/http-uploader";
import { LocationQueue, MemoryQueueStorage } from "../src/location-queue";
import { LocationSampler } from "../src/location-sampler";
import { MockLocationBackend } from "../src/mock-backend";
import type { LocationPoint, QueueStorage, Uploader } from "../src/types";

const sampleInput = { lat: 11.5564, lng: 104.9282, accuracyM: 8 };

function fixedPoint(overrides: Partial<LocationPoint> = {}): LocationPoint {
  return {
    id: "idem-fixed-1",
    lat: 11.5564,
    lng: 104.9282,
    accuracyM: 8,
    capturedAt: "2026-10-23T02:00:00.000Z",
    ...overrides
  };
}

function makeQueue(
  storage: QueueStorage,
  uploader: Uploader,
  createId?: () => string
): LocationQueue {
  return new LocationQueue({
    storage,
    uploader,
    createId,
    now: () => new Date("2026-10-23T02:00:00.000Z")
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("driver location queue", () => {
  it("persists queued points across a page reload", () => {
    const storage = new MemoryQueueStorage();
    const first = makeQueue(storage, { upload: async () => undefined }, () => "id-persist-1");
    first.enqueue(sampleInput);

    const reloaded = makeQueue(storage, { upload: async () => undefined });

    expect(reloaded.pending()).toHaveLength(1);
    expect(reloaded.pending()[0]?.id).toBe("id-persist-1");
    console.log(
      `[AC-2] queuedBeforeReload=1 queuedAfterReload=${reloaded.pending().length}`
    );
  });

  it("replays every point captured during a five minute offline window", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-23T02:00:00.000Z"));

    const storage = new MemoryQueueStorage();
    const backend = new MockLocationBackend();
    let online = false;
    let sequence = 0;

    const queue = makeQueue(storage, {
      upload: async (point) => {
        if (!online) throw new Error("network unavailable");
        backend.receive(point);
      }
    });

    const sampler = new LocationSampler({
      queue,
      intervalMs: 5_000,
      now: () => new Date(),
      getPosition: async () => {
        sequence += 1;
        return {
          lat: 11.5564 + sequence * 0.00001,
          lng: 104.9282,
          accuracyM: 6
        };
      }
    });

    sampler.start();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1_000);
    sampler.stop();

    const offlineCount = queue.pending().length;
    online = true;
    const report = await queue.flush();
    const stats = backend.stats();

    console.log(
      `[AC-3] N=${offlineCount} received=${stats.received} uniqueKeys=${stats.uniqueKeys} duplicateAttempts=${stats.duplicateAttempts} uploaded=${report.uploaded} pendingAfter=${report.pending}`
    );

    expect(offlineCount).toBe(60);
    expect(report.uploaded).toBe(offlineCount);
    expect(stats.received).toBe(offlineCount);
    expect(stats.uniqueKeys).toBe(offlineCount);
    expect(stats.duplicateAttempts).toBe(0);
    expect(queue.pending()).toHaveLength(0);
  });

  it("records a repeated idempotency key only once", () => {
    const backend = new MockLocationBackend();
    const point = fixedPoint();

    const first = backend.receive(point);
    const second = backend.receive(point);
    const stats = backend.stats();

    console.log(
      `[AC-4] first=${first.status} firstTotal=${first.total} second=${second.status} secondTotal=${second.total} finalReceived=${stats.received} duplicateAttempts=${stats.duplicateAttempts}`
    );

    expect(first.status).toBe("accepted");
    expect(second.status).toBe("duplicate");
    expect(stats.received).toBe(1);
    expect(stats.uniqueKeys).toBe(1);
    expect(stats.duplicateAttempts).toBe(1);
  });

  it("does not upload twice when flush is called concurrently", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    let calls = 0;
    const storage = new MemoryQueueStorage();
    let nextId = 0;
    const queue = makeQueue(
      storage,
      {
        upload: async () => {
          calls += 1;
          await gate;
        }
      },
      () => `id-concurrent-${++nextId}`
    );
    queue.enqueue(sampleInput);
    queue.enqueue(sampleInput);

    const first = queue.flush();
    const second = queue.flush();
    release();
    await Promise.all([first, second]);

    expect(calls).toBe(2);
    expect(queue.pending()).toHaveLength(0);
  });

  it("keeps points queued when upload fails", async () => {
    const storage = new MemoryQueueStorage();
    const queue = makeQueue(storage, {
      upload: async () => {
        throw new Error("offline");
      }
    });
    queue.enqueue(sampleInput);
    queue.enqueue(sampleInput);

    const report = await queue.flush();

    expect(report.uploaded).toBe(0);
    expect(report.pending).toBe(2);
    expect(queue.pending()).toHaveLength(2);
  });

  it("sends the point id as the Idempotency-Key header", async () => {
    const calls: Array<{ url: string; init: RequestInit }> = [];
    const uploader = createHttpUploader(
      "http://mock.local/v1/driver/locations",
      async (url, init) => {
        calls.push({ url: String(url), init: init ?? {} });
        return new Response("{}", { status: 201 });
      }
    );

    await uploader.upload(fixedPoint());

    const headers = new Headers(calls[0]?.init.headers);
    expect(headers.get("idempotency-key")).toBe("idem-fixed-1");
    expect(headers.get("content-type")).toBe("application/json");
  });
});
