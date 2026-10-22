import { check } from "k6";

import { verifyAll } from "./checks.js";

export function record(res, label, assertions) {
  for (const result of verifyAll(res, label, assertions)) {
    check(res, { [result.name]: () => result.passed });
    if (!result.passed) {
      console.error(`${result.name}: ${result.error}`);
    }
  }
}

export function bearerHeaders(token) {
  return { Authorization: `Bearer ${token}` };
}

export function jsonHeaders(token) {
  const headers = { "Content-Type": "application/json" };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

export function unwrapList(raw, label) {
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    throw new Error(`${label} is not valid JSON: ${error.message}`);
  }
  if (Array.isArray(parsed)) {
    return parsed;
  }
  if (parsed && typeof parsed === "object") {
    for (const key of ["items", "bodies", "init_data", "payloads"]) {
      if (Array.isArray(parsed[key])) {
        return parsed[key];
      }
    }
  }
  throw new Error(`${label} must be a JSON array or an object with an items/bodies/init_data/payloads array`);
}
