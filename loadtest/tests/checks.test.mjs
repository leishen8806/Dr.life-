import test from "node:test";
import assert from "node:assert/strict";

import {
  CheckError,
  assertHealthz,
  assertNoFingerprint,
  assertPermissionDenied,
  assertPublicPing,
  assertReadyz,
  assertSecurityHeaders,
  assertUnauthenticated,
  headerValue,
} from "../lib/checks.js";

const headers = {
  "Content-Type": "application/json",
  "X-Content-Type-Options": "nosniff",
  "Referrer-Policy": "no-referrer",
  "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
  "Cross-Origin-Resource-Policy": "same-site",
  "Cache-Control": "no-store",
};

const response = (status, body, extraHeaders = {}) => ({
  status,
  headers: { ...headers, ...extraHeaders },
  body: JSON.stringify(body),
  json: () => JSON.parse(JSON.stringify(body)),
});

test("accepts the baseline health response", () => {
  assert.doesNotThrow(() => assertHealthz(response(200, { ok: true })));
});

test("mutation: healthz turns red when ok is not exactly true", () => {
  assert.throws(() => assertHealthz(response(200, { ok: false })), CheckError);
  assert.throws(() => assertHealthz(response(200, {})), CheckError);
});

test("mutation: missing target response turns red instead of passing through optional chaining", () => {
  assert.throws(() => assertHealthz(undefined), CheckError);
  assert.throws(() => assertHealthz({}), CheckError);
});

test("mutation: non-JSON health content type turns red", () => {
  assert.throws(
    () => assertHealthz(response(200, { ok: true }, { "Content-Type": "text/plain" })),
    CheckError,
  );
});

test("accepts the baseline readiness envelope", () => {
  assert.doesNotThrow(() =>
    assertReadyz(response(200, { ok: true, data: { ready: true }, request_id: "req-1" })),
  );
});

test("mutation: readiness without data or request_id turns red", () => {
  assert.throws(() => assertReadyz(response(200, { ok: true })), CheckError);
  assert.throws(
    () => assertReadyz(response(200, { ok: true, data: { ready: true } })),
    CheckError,
  );
  assert.throws(
    () => assertReadyz(response(200, { ok: true, data: { ready: false }, request_id: "r" })),
    CheckError,
  );
});

test("accepts the public ping envelope", () => {
  assert.doesNotThrow(() =>
    assertPublicPing(response(200, { ok: true, data: { pong: true }, request_id: "r" })),
  );
});

test("mutation: ping without data.pong turns red", () => {
  assert.throws(() => assertPublicPing(response(200, { ok: true, data: {}, request_id: "r" })), CheckError);
  assert.throws(() => assertPublicPing(response(200, { ok: true })), CheckError);
});

test("accepts a 401 unauthenticated envelope with a bearer challenge", () => {
  assert.doesNotThrow(() =>
    assertUnauthenticated(
      response(
        401,
        { ok: false, error: { code: 2001, message: { en: "Authentication required" } }, request_id: "r" },
        { "WWW-Authenticate": "Bearer" },
      ),
    ),
  );
});

test("mutation: 401 without the challenge header turns red", () => {
  assert.throws(
    () =>
      assertUnauthenticated(
        response(401, { ok: false, error: { code: 2001 }, request_id: "r" }),
      ),
    CheckError,
  );
});

test("mutation: wrong error code turns red", () => {
  assert.throws(
    () =>
      assertUnauthenticated(
        response(401, { ok: false, error: { code: 2002 }, request_id: "r" }, { "WWW-Authenticate": "Bearer" }),
      ),
    CheckError,
  );
});

test("accepts the gateway permission-denied envelope", () => {
  assert.doesNotThrow(() =>
    assertPermissionDenied(
      response(403, { ok: false, error: { code: 2002, message: { en: "Permission denied" } }, request_id: "r" }),
    ),
  );
});

test("mutation: 404 does not satisfy the unknown-audience contract", () => {
  assert.throws(
    () => assertPermissionDenied(response(404, { ok: false, error: { code: 1003 }, request_id: "r" })),
    CheckError,
  );
});

test("accepts the complete security header set", () => {
  assert.doesNotThrow(() => assertSecurityHeaders(response(200, { ok: true })));
});

for (const name of [
  "X-Content-Type-Options",
  "Referrer-Policy",
  "Content-Security-Policy",
  "Cross-Origin-Resource-Policy",
  "Cache-Control",
]) {
  test("mutation: removing " + name + " turns the guard red", () => {
    const res = response(200, { ok: true });
    delete res.headers[name];
    assert.throws(() => assertSecurityHeaders(res), CheckError);
  });
}

test("mutation: a weakened content security policy turns red", () => {
  const res = response(200, { ok: true }, { "Content-Security-Policy": "default-src *" });
  assert.throws(() => assertSecurityHeaders(res), CheckError);
});

test("accepts responses without server fingerprints", () => {
  assert.doesNotThrow(() => assertNoFingerprint(response(200, { ok: true })));
});

test("mutation: Server or X-Powered-By turns the fingerprint guard red", () => {
  assert.throws(
    () => assertNoFingerprint(response(200, { ok: true }, { Server: "nginx" })),
    CheckError,
  );
  assert.throws(
    () => assertNoFingerprint(response(200, { ok: true }, { "X-Powered-By": "PHP/8" })),
    CheckError,
  );
});

test("header lookup is case-insensitive and handles array values", () => {
  assert.equal(headerValue({ "www-authenticate": "Bearer" }, "WWW-Authenticate"), "Bearer");
  assert.equal(headerValue({ "WWW-Authenticate": ["Bearer", "Basic"] }, "www-authenticate"), "Bearer, Basic");
  assert.equal(headerValue({}, "WWW-Authenticate"), undefined);
});

test("mutation: a response without headers turns red before body checks", () => {
  assert.throws(() => assertSecurityHeaders({ status: 200, body: "{}" }), CheckError);
});
