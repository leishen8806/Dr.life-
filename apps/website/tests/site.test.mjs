import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const dist = fileURLToPath(new URL("../dist/", import.meta.url));

test("the static build exposes the required public routes", () => {
  for (const page of ["index.html", "privacy/index.html", "terms/index.html"]) {
    assert.equal(existsSync(join(dist, page)), true, `${page} is missing`);
  }
});

test("the static build contains no prohibited claims and every internal link resolves", () => {
  const prohibited = ["24h 仓配无忧", "使命必达", "全柬配送", "一仓入柬、全柬销售", "精装交付与监理", "质量监理", "官方质检验收试点平台", "网商贷"];
  const htmlFiles = [];
  const collect = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) collect(path);
      if (entry.name.endsWith(".html")) htmlFiles.push(path);
    }
  };
  collect(dist);
  for (const file of htmlFiles) {
    const html = readFileSync(file, "utf8");
    for (const phrase of prohibited) assert.equal(html.includes(phrase), false, `${phrase} found in ${file}`);
    for (const href of html.matchAll(/href="(\/[^"]*)"/g)) {
      const [path] = href[1].split(/[?#]/);
      assert.equal(existsSync(join(dist, path, "index.html")) || existsSync(join(dist, path)), true, `${href[1]} from ${file} is unreachable`);
    }
  }
});
