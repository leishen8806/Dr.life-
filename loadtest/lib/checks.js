export class CheckError extends Error {
  constructor(message) {
    super(message);
    this.name = "CheckError";
  }
}

function fail(message) {
  throw new CheckError(message);
}

export function headerValue(headers, name) {
  if (!headers || typeof headers !== "object") {
    return undefined;
  }
  const wanted = String(name).toLowerCase();
  for (const key of Object.keys(headers)) {
    if (key.toLowerCase() !== wanted) {
      continue;
    }
    const value = headers[key];
    if (Array.isArray(value)) {
      return value.join(", ");
    }
    if (value === undefined || value === null) {
      return undefined;
    }
    return String(value);
  }
  return undefined;
}

export function requireResponse(res, label) {
  if (!res || typeof res !== "object") {
    fail(`${label}: response is missing`);
  }
  if (typeof res.status !== "number" || !Number.isInteger(res.status)) {
    fail(`${label}: response status is missing or not an integer`);
  }
  if (!res.headers || typeof res.headers !== "object") {
    fail(`${label}: response headers are missing`);
  }
  if (typeof res.body !== "string") {
    fail(`${label}: response body is missing`);
  }
}

export function parseJsonBody(res, label) {
  requireResponse(res, label);
  let body;
  try {
    body = typeof res.json === "function" ? res.json() : JSON.parse(res.body);
  } catch (error) {
    fail(`${label}: response body is not valid JSON (${error.message})`);
  }
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    fail(`${label}: response body must be a JSON object`);
  }
  return body;
}

export function assertStatus(res, expected, label) {
  requireResponse(res, label);
  if (res.status !== expected) {
    fail(`${label}: expected HTTP ${expected}, got ${res.status}`);
  }
}

export function assertStatusIn(res, expected, label) {
  requireResponse(res, label);
  if (!Array.isArray(expected) || expected.length === 0) {
    fail(`${label}: expected status list must not be empty`);
  }
  if (!expected.includes(res.status)) {
    fail(`${label}: expected one of ${expected.join(", ")}, got ${res.status}`);
  }
}

export function assert2xx(res, label) {
  requireResponse(res, label);
  if (res.status < 200 || res.status >= 300) {
    fail(`${label}: expected a 2xx response, got ${res.status}`);
  }
}

export function assertJsonOk(res, label) {
  const body = parseJsonBody(res, label);
  if (body.ok !== true) {
    fail(`${label}: expected ok=true`);
  }
  return body;
}

export function assertRetryAfter(res, label, minimum, maximum) {
  requireResponse(res, label);
  const min = minimum === undefined ? 1 : minimum;
  const max = maximum === undefined ? 3 : maximum;
  const raw = headerValue(res.headers, "Retry-After");
  if (raw === undefined) {
    fail(`${label}: Retry-After is required`);
  }
  const text = String(raw).trim();
  if (!/^[1-9]\d*$/.test(text)) {
    fail(`${label}: Retry-After must be a positive integer number of seconds, got "${text}"`);
  }
  const seconds = Number(text);
  if (seconds < min || seconds > max) {
    fail(`${label}: Retry-After must be between ${min} and ${max} seconds, got ${seconds}`);
  }
  return seconds;
}

export function assertNoDeadlock(res, label) {
  requireResponse(res, label);
  const text = String(res.body).toLowerCase();
  if (text.includes("deadlock") || text.includes("40p01")) {
    fail(`${label}: response reports a database deadlock`);
  }
}

export function assertErrorEnvelope(res, expectedStatus, expectedCode, label) {
  assertStatus(res, expectedStatus, label);
  const body = parseJsonBody(res, label);
  if (body.ok !== false) {
    fail(`${label}: expected ok=false`);
  }
  if (!body.error || typeof body.error !== "object" || Array.isArray(body.error)) {
    fail(`${label}: error envelope is missing`);
  }
  if (body.error.code !== expectedCode) {
    fail(`${label}: expected error code ${expectedCode}, got ${String(body.error.code)}`);
  }
  if (typeof body.request_id !== "string" || body.request_id.trim() === "") {
    fail(`${label}: request_id is missing`);
  }
  return body;
}

export function assertHealthz(res) {
  assertStatus(res, 200, "healthz");
  const contentType = headerValue(res.headers, "Content-Type");
  if (!contentType || !contentType.toLowerCase().startsWith("application/json")) {
    fail(`healthz: expected application/json content type, got ${String(contentType)}`);
  }
  const body = parseJsonBody(res, "healthz");
  if (body.ok !== true) {
    fail("healthz: expected ok=true");
  }
}

export function assertReadyz(res) {
  assertStatus(res, 200, "readyz");
  const body = parseJsonBody(res, "readyz");
  if (body.ok !== true) {
    fail("readyz: expected ok=true");
  }
  if (!body.data || typeof body.data !== "object" || Array.isArray(body.data)) {
    fail("readyz: data envelope is missing");
  }
  if (body.data.ready !== true) {
    fail("readyz: expected data.ready=true");
  }
  if (typeof body.request_id !== "string" || body.request_id.trim() === "") {
    fail("readyz: request_id is missing");
  }
}

export function assertPublicPing(res) {
  assertStatus(res, 200, "public ping");
  const body = parseJsonBody(res, "public ping");
  if (body.ok !== true) {
    fail("public ping: expected ok=true");
  }
  if (!body.data || typeof body.data !== "object" || Array.isArray(body.data)) {
    fail("public ping: data envelope is missing");
  }
  if (body.data.pong !== true) {
    fail("public ping: expected data.pong=true");
  }
  if (typeof body.request_id !== "string" || body.request_id.trim() === "") {
    fail("public ping: request_id is missing");
  }
}

export function assertUnauthenticated(res) {
  const challenge = headerValue(res.headers, "WWW-Authenticate");
  if (!challenge || !challenge.toLowerCase().includes("bearer")) {
    fail("unauthenticated: WWW-Authenticate must offer Bearer");
  }
  assertErrorEnvelope(res, 401, 2001, "unauthenticated");
}

export function assertPermissionDenied(res) {
  assertErrorEnvelope(res, 403, 2002, "permission denied");
}

export function assertSecurityHeaders(res) {
  requireResponse(res, "security headers");
  const required = [
    ["X-Content-Type-Options", "nosniff"],
    ["Referrer-Policy", "no-referrer"],
    ["Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'"],
    ["Cross-Origin-Resource-Policy", "same-site"],
    ["Cache-Control", "no-store"],
  ];
  for (const [name, expected] of required) {
    const actual = headerValue(res.headers, name);
    if (actual !== expected) {
      fail(`security headers: ${name} must be "${expected}", got ${String(actual)}`);
    }
  }
}

export function assertNoFingerprint(res) {
  requireResponse(res, "fingerprint headers");
  const server = headerValue(res.headers, "Server");
  if (server !== undefined) {
    fail(`fingerprint headers: Server must be absent, got ${server}`);
  }
  const poweredBy = headerValue(res.headers, "X-Powered-By");
  if (poweredBy !== undefined) {
    fail(`fingerprint headers: X-Powered-By must be absent, got ${poweredBy}`);
  }
}

export function verifyAll(res, label, assertions) {
  const results = [];
  for (const [name, assertion] of assertions) {
    try {
      assertion(res);
      results.push({ name: `${label}: ${name}`, passed: true });
    } catch (error) {
      results.push({ name: `${label}: ${name}`, passed: false, error: error.message });
    }
  }
  return results;
}
