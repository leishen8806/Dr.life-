import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { findHardcodedUserText, validateDictionaries } from "./check-i18n.mjs";

const valid = {
  en: { greeting: "Hello {name}" },
  zh: { greeting: "你好 {name}" },
  km: { greeting: "[TODO-KM-REVIEW] សួស្តី {name}" }
};

test("rejects missing, extra, empty, and mismatched placeholder entries", () => {
  const cases = [
    { en: { key: "ok" }, zh: {}, km: { key: "ok" } },
    { en: { key: "ok" }, zh: { key: "ok", extra: "x" }, km: { key: "ok" } },
    { en: { key: "ok" }, zh: { key: " " }, km: { key: "ok" } },
    { en: { key: "{name}" }, zh: { key: "{other}" }, km: { key: "{name}" } }
  ];

  for (const dictionaries of cases) {
    assert.notEqual(validateDictionaries(dictionaries).failures.length, 0);
  }
});

test("accepts matching dictionaries and reports Khmer review markers", () => {
  const result = validateDictionaries(valid);
  assert.deepEqual(result.failures, []);
  assert.deepEqual(result.reviewKeys, ["greeting"]);
});

test("rejects hard-coded CJK or Khmer text in application TSX", async () => {
  const root = await mkdtemp(join(tmpdir(), "drlife-i18n-"));
  try {
    await mkdir(join(root, "apps", "demo"), { recursive: true });
    await mkdir(join(root, "packages", "ui"), { recursive: true });
    await writeFile(
      join(root, "apps", "demo", "main.tsx"),
      "export const view = <button>确认</button>;\n"
    );
    assert.equal((await findHardcodedUserText(root)).length, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
