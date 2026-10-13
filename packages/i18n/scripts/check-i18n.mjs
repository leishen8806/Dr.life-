import { readdir, readFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const languages = ["zh", "en", "km"];
const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const repositoryRoot = dirname(dirname(packageRoot));
const localeDirectory = join(packageRoot, "src", "locales");
const placeholderPattern = /\{([A-Za-z][A-Za-z0-9_]*)\}/g;

function placeholders(value) {
  return [...value.matchAll(placeholderPattern)]
    .map((match) => match[1])
    .sort();
}

export function validateDictionaries(dictionaries) {
  const referenceKeys = Object.keys(dictionaries.en).sort();
  const failures = [];
  const reviewKeys = [];
  const warnings = [];

  for (const language of languages) {
    const dictionary = dictionaries[language];
    const keys = Object.keys(dictionary).sort();
    const missing = referenceKeys.filter((key) => !(key in dictionary));
    const extra = keys.filter((key) => !referenceKeys.includes(key));
    if (missing.length)
      failures.push(`${language}: missing ${missing.join(", ")}`);
    if (extra.length) failures.push(`${language}: extra ${extra.join(", ")}`);

    for (const key of referenceKeys) {
      const value = dictionary[key];
      if (typeof value !== "string" || value.trim() === "") {
        failures.push(`${language}.${key}: empty value`);
        continue;
      }
      if (
        placeholders(value).join(",") !==
        placeholders(dictionaries.en[key]).join(",")
      ) {
        failures.push(`${language}.${key}: placeholders differ from en`);
      }
      if (language === "km" && value.startsWith("[TODO-KM-REVIEW] ")) {
        reviewKeys.push(key);
      }
    }
  }

  for (const key of referenceKeys) {
    const khmer = dictionaries.km[key].replace(/^\[TODO-KM-REVIEW\] /, "");
    const english = dictionaries.en[key];
    if (khmer.length > 60 || khmer.length > english.length * 2.5) {
      warnings.push(
        `km.${key}: ${khmer.length} chars versus en ${english.length}; check available width`
      );
    }
  }

  return { failures, referenceKeys, reviewKeys, warnings };
}

async function sourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...(await sourceFiles(path)));
    else if (entry.name.endsWith(".tsx") && !entry.name.includes(".test.")) {
      files.push(path);
    }
  }
  return files;
}

export async function findHardcodedUserText(root = repositoryRoot) {
  const roots = [join(root, "apps"), join(root, "packages", "ui")];
  const failures = [];
  for (const directory of roots) {
    for (const file of await sourceFiles(directory)) {
      const lines = (await readFile(file, "utf8")).split(/\r?\n/);
      lines.forEach((line, index) => {
        const jsxText = line.match(
          />[^<{]*[\u3400-\u9fff\u1780-\u17ff][^<{]*</u
        );
        const stringLiteral = line.match(
          /["'`][^"'`]*[\u3400-\u9fff\u1780-\u17ff][^"'`]*["'`]/u
        );
        if (jsxText || stringLiteral) {
          failures.push(
            `${relative(root, file)}:${index + 1}: hard-coded user-visible CJK/Khmer text`
          );
        }
      });
    }
  }
  return failures;
}

async function main() {
  const dictionaries = Object.fromEntries(
    await Promise.all(
      languages.map(async (language) => [
        language,
        JSON.parse(
          await readFile(join(localeDirectory, `${language}.json`), "utf8")
        )
      ])
    )
  );
  const result = validateDictionaries(dictionaries);
  result.failures.push(...(await findHardcodedUserText()));

  for (const warning of result.warnings) console.warn(`warning: ${warning}`);
  if (result.failures.length) {
    console.error(result.failures.join("\n"));
    process.exitCode = 1;
    return;
  }

  console.log(`i18n dictionaries match (${result.referenceKeys.length} keys).`);
  console.log(
    `TODO-KM-REVIEW: ${result.reviewKeys.length}${
      result.reviewKeys.length ? ` (${result.reviewKeys.join(", ")})` : ""
    }`
  );
  console.log(
    "No hard-coded CJK/Khmer user-visible text found in apps or packages/ui."
  );
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  await main();
}
