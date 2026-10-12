import { readFile } from "node:fs/promises";

const languages = ["zh", "en", "km"];
const directory = new URL("../src/locales/", import.meta.url);
const dictionaries = Object.fromEntries(
  await Promise.all(
    languages.map(async (language) => [
      language,
      JSON.parse(await readFile(new URL(`${language}.json`, directory), "utf8"))
    ])
  )
);
const referenceKeys = Object.keys(dictionaries.en).sort();
const placeholderPattern = /\{([A-Za-z][A-Za-z0-9_]*)\}/g;
const failures = [];
const reviewKeys = [];

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
    const placeholders = [...value.matchAll(placeholderPattern)]
      .map((match) => match[1])
      .sort();
    const expected = [...dictionaries.en[key].matchAll(placeholderPattern)]
      .map((match) => match[1])
      .sort();
    if (placeholders.join(",") !== expected.join(",")) {
      failures.push(`${language}.${key}: placeholders differ from en`);
    }
    if (language === "km" && value.startsWith("[TODO-KM-REVIEW] "))
      reviewKeys.push(key);
  }
}

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}

console.log(`i18n dictionaries match (${referenceKeys.length} keys).`);
console.log(
  `TODO-KM-REVIEW: ${reviewKeys.length}${reviewKeys.length ? ` (${reviewKeys.join(", ")})` : ""}`
);
