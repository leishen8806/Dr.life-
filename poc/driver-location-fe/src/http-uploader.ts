import type { LocationPoint, Uploader } from "./types";

export type FetchLike = (
  input: RequestInfo | URL,
  init?: RequestInit
) => Promise<Response>;

export function createHttpUploader(
  endpoint: string,
  fetchImpl: FetchLike = fetch
): Uploader {
  return {
    async upload(point: LocationPoint): Promise<void> {
      const response = await fetchImpl(endpoint, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          "idempotency-key": point.id
        },
        body: JSON.stringify(point),
        cache: "no-store"
      });
      if (!response.ok) {
        throw new Error(`location upload failed with HTTP ${response.status}`);
      }
    }
  };
}
