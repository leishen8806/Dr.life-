export interface PositionSample {
  lat: number;
  lng: number;
  accuracyM: number;
}

export interface LocationPoint extends PositionSample {
  id: string;
  capturedAt: string;
}

export interface QueueStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface Uploader {
  upload(point: LocationPoint): Promise<void>;
}

export interface FlushReport {
  attempted: number;
  uploaded: number;
  pending: number;
}
