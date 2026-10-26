import type { LocationPoint } from "./types";

export interface MockBackendStats {
  received: number;
  uniqueKeys: number;
  duplicateAttempts: number;
}

export type ReceiveResult =
  | { status: "accepted" | "duplicate"; id: string; total: number }
  | { status: "invalid"; total: number; error: string };

export class MockLocationBackend {
  private readonly points = new Map<string, LocationPoint>();
  private duplicateAttempts = 0;

  receive(input: unknown): ReceiveResult {
    if (!isLocationPoint(input)) {
      return {
        status: "invalid",
        total: this.points.size,
        error: "invalid location point"
      };
    }

    if (this.points.has(input.id)) {
      this.duplicateAttempts += 1;
      return { status: "duplicate", id: input.id, total: this.points.size };
    }

    this.points.set(input.id, { ...input });
    return { status: "accepted", id: input.id, total: this.points.size };
  }

  stats(): MockBackendStats {
    return {
      received: this.points.size,
      uniqueKeys: this.points.size,
      duplicateAttempts: this.duplicateAttempts
    };
  }

  snapshot(): LocationPoint[] {
    return [...this.points.values()].map((point) => ({ ...point }));
  }

  reset(): void {
    this.points.clear();
    this.duplicateAttempts = 0;
  }
}

function isLocationPoint(value: unknown): value is LocationPoint {
  if (!value || typeof value !== "object") return false;
  const point = value as Partial<LocationPoint>;
  return (
    typeof point.id === "string" &&
    point.id.length > 0 &&
    point.id.length <= 128 &&
    typeof point.lat === "number" &&
    Number.isFinite(point.lat) &&
    point.lat >= -90 &&
    point.lat <= 90 &&
    typeof point.lng === "number" &&
    Number.isFinite(point.lng) &&
    point.lng >= -180 &&
    point.lng <= 180 &&
    typeof point.accuracyM === "number" &&
    Number.isFinite(point.accuracyM) &&
    point.accuracyM >= 0 &&
    typeof point.capturedAt === "string" &&
    !Number.isNaN(Date.parse(point.capturedAt))
  );
}
