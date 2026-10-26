import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { MockLocationBackend } from "../src/mock-backend.ts";

const MAX_BODY_BYTES = 64 * 1024;
const port = Number(process.env.MOCK_PORT ?? 8787);
const backend = new MockLocationBackend();

const server = createServer((request, response) => {
  void handle(request, response).catch((error: unknown) => {
    console.error("[mock-server]", error);
    send(response, 500, { error: "internal mock server error" });
  });
});

async function handle(
  request: IncomingMessage,
  response: ServerResponse
): Promise<void> {
  const url = new URL(request.url ?? "/", "http://127.0.0.1");

  if (request.method === "OPTIONS") {
    send(response, 204, undefined);
    return;
  }

  if (request.method === "GET" && url.pathname === "/__mock/health") {
    send(response, 200, { ok: true });
    return;
  }

  if (request.method === "GET" && url.pathname === "/__mock/stats") {
    send(response, 200, {
      ...backend.stats(),
      keys: backend.snapshot().map((point) => point.id)
    });
    return;
  }

  if (request.method === "POST" && url.pathname === "/__mock/reset") {
    backend.reset();
    send(response, 200, { ok: true });
    return;
  }

  if (request.method === "POST" && url.pathname === "/v1/driver/locations") {
    const idempotencyKey = headerValue(request.headers["idempotency-key"]);
    if (!idempotencyKey) {
      send(response, 400, { error: "missing Idempotency-Key header" });
      return;
    }

    let body: unknown;
    try {
      body = await readJson(request);
    } catch (error) {
      send(response, 400, {
        error: error instanceof Error ? error.message : "invalid request body"
      });
      return;
    }

    if (
      !body ||
      typeof body !== "object" ||
      (body as { id?: unknown }).id !== idempotencyKey
    ) {
      send(response, 400, {
        error: "Idempotency-Key must match body.id"
      });
      return;
    }

    const result = backend.receive(body);
    const status =
      result.status === "accepted"
        ? 201
        : result.status === "duplicate"
          ? 200
          : 400;
    console.log(
      `${new Date().toISOString()} POST /v1/driver/locations id=${idempotencyKey.slice(0, 8)} status=${result.status} total=${result.total}`
    );
    send(response, status, result);
    return;
  }

  send(response, 404, { error: "not found" });
}

function readJson(request: IncomingMessage): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let size = 0;

    request.on("data", (chunk: Buffer) => {
      size += chunk.length;
      if (size > MAX_BODY_BYTES) {
        reject(new Error("request body too large"));
        request.destroy();
        return;
      }
      chunks.push(chunk);
    });
    request.on("end", () => {
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString("utf8")));
      } catch {
        reject(new Error("invalid JSON body"));
      }
    });
    request.on("error", reject);
  });
}

function headerValue(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

function send(
  response: ServerResponse,
  status: number,
  body: unknown
): void {
  if (response.headersSent) return;
  const payload = body === undefined ? "" : JSON.stringify(body);
  response.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(payload),
    "access-control-allow-origin": "*",
    "access-control-allow-headers": "content-type, idempotency-key",
    "access-control-allow-methods": "GET,POST,OPTIONS",
    "cache-control": "no-store"
  });
  response.end(payload);
}

server.listen(port, "127.0.0.1", () => {
  console.log(`mock backend listening on http://127.0.0.1:${port}`);
});

process.on("SIGTERM", () => {
  server.close(() => process.exit(0));
});
