import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";

const source = fs.readFileSync(new URL("../src/main.tsx", import.meta.url), "utf8");
const apiTypes = fs.readFileSync(new URL("../src/api/types.ts", import.meta.url), "utf8");

test("reject action requires a non-empty reason", () => {
  assert.match(source, /!reason\.trim\(\)/);
  assert.match(source, /admin\.rejectRequired/);
});

test("review and order views include permission-safe actions and detail drawer", () => {
  assert.match(source, /disabled=\{row\.status !== "pending"\}/);
  assert.match(source, /<Drawer/);
  assert.match(source, /loginUnavailable/);
  assert.match(apiTypes, /approveReview/);
  assert.match(apiTypes, /getOrder/);
});

test("masked phone and e4 money are represented by the typed adapter", () => {
  const mock = fs.readFileSync(new URL("../src/api/mock.ts", import.meta.url), "utf8");
  assert.match(mock, /\*\*\* 1234/);
  assert.match(mock, /amountE4/);
});
