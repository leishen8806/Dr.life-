import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";
import { ApiError } from "./errors";
import { createIdentifier, createResilientFetch } from "./http";

export type { components, operations, paths } from "./schema";
export { ApiError } from "./errors";
export { CircuitOpenError, createIdempotentAction, createResilientFetch, loginStormDelay, redactSensitive, retryDelay } from "./http";
export type Language = "zh" | "en" | "km";
type ErrorEnvelope = components["schemas"]["ErrorEnvelope"];

export function withIdempotencyKey(): { "Idempotency-Key": string } {
  return { "Idempotency-Key": createIdentifier() };
}

export interface ApiClientOptions {
  baseUrl?: string;
  getToken?: () => string | null | undefined;
  getLanguage?: () => Language;
  fetch?: typeof globalThis.fetch;
  resilience?: Parameters<typeof createResilientFetch>[1];
}

function defaultBaseUrl(): string {
  if (import.meta.env.VITE_API_BASE_URL)
    return import.meta.env.VITE_API_BASE_URL;
  if (import.meta.env.DEV || import.meta.env.MODE === "test")
    return "http://localhost:8080";
  throw new Error("VITE_API_BASE_URL is required outside development");
}

function isErrorEnvelope(value: unknown): value is ErrorEnvelope {
  if (!value || typeof value !== "object") return false;
  const envelope = value as Partial<ErrorEnvelope>;
  return (
    envelope.ok === false &&
    !!envelope.error &&
    typeof envelope.request_id === "string"
  );
}

export function createApiClient(options: ApiClientOptions = {}) {
  const client = createClient<paths>({
    baseUrl: options.baseUrl ?? defaultBaseUrl(),
    fetch: createResilientFetch(options.fetch ?? globalThis.fetch, options.resilience)
  });
  const middleware: Middleware = {
    async onRequest({ request }) {
      const token = options.getToken?.();
      if (token) request.headers.set("Authorization", `Bearer ${token}`);
      request.headers.set("X-Request-ID", createIdentifier());
      request.headers.set("Accept-Language", options.getLanguage?.() ?? "en");
      return request;
    },
    async onResponse({ response }) {
      if (response.ok) return response;
      const body: unknown = await response
        .clone()
        .json()
        .catch(() => undefined);
      if (isErrorEnvelope(body)) {
        throw new ApiError(body.error.code, body.request_id, response.status);
      }
      return response;
    }
  };
  client.use(middleware);
  return client;
}
