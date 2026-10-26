import { LocationQueue } from "./location-queue";
import type { FlushReport, LocationPoint, PositionSample } from "./types";

export type SamplerEvent =
  | { type: "captured"; point: LocationPoint }
  | { type: "flush"; report: FlushReport }
  | { type: "capture-error"; error: unknown }
  | { type: "flush-error"; error: unknown };

export interface LocationSamplerOptions {
  queue: LocationQueue;
  intervalMs: number;
  getPosition: () => Promise<PositionSample>;
  now?: () => Date;
  onEvent?: (event: SamplerEvent) => void;
}

export class LocationSampler {
  private readonly queue: LocationQueue;
  private readonly intervalMs: number;
  private readonly getPosition: () => Promise<PositionSample>;
  private readonly now: () => Date;
  private readonly onEvent: (event: SamplerEvent) => void;
  private timer: ReturnType<typeof setInterval> | null = null;
  private busy = false;

  constructor(options: LocationSamplerOptions) {
    this.queue = options.queue;
    this.intervalMs = options.intervalMs;
    this.getPosition = options.getPosition;
    this.now = options.now ?? (() => new Date());
    this.onEvent = options.onEvent ?? (() => undefined);
  }

  start(): void {
    if (this.timer) return;
    this.timer = setInterval(() => {
      void this.tick();
    }, this.intervalMs);
  }

  stop(): void {
    if (!this.timer) return;
    clearInterval(this.timer);
    this.timer = null;
  }

  async tick(): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      const position = await this.getPosition();
      const point = this.queue.enqueue({
        ...position,
        capturedAt: this.now().toISOString()
      });
      this.onEvent({ type: "captured", point });
      try {
        const report = await this.queue.flush();
        this.onEvent({ type: "flush", report });
      } catch (error) {
        this.onEvent({ type: "flush-error", error });
      }
    } catch (error) {
      this.onEvent({ type: "capture-error", error });
    } finally {
      this.busy = false;
    }
  }
}
