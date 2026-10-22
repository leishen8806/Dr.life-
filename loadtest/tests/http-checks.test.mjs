import test from "node:test";
import assert from "node:assert/strict";

import {
  CheckError,
  assert2xx,
  assertJsonOk,
  assertNoDeadlock,
  assertRetryAfter,
  assertStatusIn,
} from "../lib/checks.js";

const response = (status, body, extraHeaders = {}) => ({
  status,
  headers: { "Content-Type": "application/json", ...extraHeaders },
  body: typeof body === "string" ? body : JSON.stringify(body),
  json: () => (typeof body === "string" ? JSON.parse(body) : JSON.parse(JSON.stringify(body))),
});

test("assertStatusIn accepts an expected status and rejects another", () => {
  assert.doesNotThrow(() => assertStatusIn(response(201, { ok: true }), [200, 201, 202], "write"));
  assert.throws(() => assertStatusIn(response(500, { ok: false }), [200, 201, 202], "write"), CheckError);
});

test("mutation: an empty expected-status list turns red instead of accepting anything", () => {
  assert.throws(() => assertStatusIn(response(200, { ok: true }), [], "write"), CheckError);
});

test("assert2xx accepts 200, 201, and 204", () => {
  for (const status of [200, 201, 204]) {
    assert.doesNotThrow(() => assert2xx(response(status, { ok: true }), "write"));
  }
});

test("mutation: 3xx, 4xx, and 5xx turns the 2xx guard red", () => {
  for (const status of [302, 400, 401, 422, 500, 503]) {
    assert.throws(() => assert2xx(response(status, { ok: true }), "write"), CheckError);
  }
});

test("assertJsonOk requires ok:true", () => {
  assert.doesNotThrow(() => assertJsonOk(response(200, { ok: true }), "write"));
  assert.throws(() => assertJsonOk(response(200, { ok: false }), "write"), CheckError);
  assert.throws(() => assertJsonOk(response(200, {}), "write"), CheckError);
});

test("assertRetryAfter accepts an integer in the documented 1-3 second window", () => {
  for (const seconds of ["1", "2", "3"]) {
    assert.equal(assertRetryAfter(response(503, { ok: false }, { "Retry-After": seconds }), "rejection"), Number(seconds));
  }
});

test("mutation: a missing, zero, fractional, or out-of-range Retry-After turns red", () => {
  for (const seconds of [undefined, "0", "1.5", "4", "", "abc"]) {
    const headers = seconds === undefined ? {} : { "Retry-After": seconds };
    assert.throws(() => assertRetryAfter(response(503, { ok: false }, headers), "rejection"), CheckError);
  }
});

test("assertNoDeadlock accepts normal error bodies and rejects a Postgres deadlock", () => {
  assert.doesNotThrow(() => assertNoDeadlock(response(409, { ok: false, error: { code: 2003 } }), "write"));
  assert.throws(() => assertNoDeadlock(response(500, { ok: false, error: { message: "deadlock detected" } }), "write"), CheckError);
  assert.throws(() => assertNoDeadlock(response(500, { ok: false, error: { message: "SQLSTATE 40P01" } }), "write"), CheckError);
});
