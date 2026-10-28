import net from "node:net";
import http from "node:http";

import { validateTargetEnv } from "../lib/guard.js";

let target;
try {
  target = validateTargetEnv(process.env);
} catch (error) {
  console.error(`GUARD: ${error.message}`);
  process.exit(1);
}

const sockets = Number(String(process.env.SLOWLORIS_SOCKETS || "64").trim());
const holdMs = Number(String(process.env.SLOWLORIS_HOLD_MS || "3000").trim());
if (!Number.isInteger(sockets) || sockets < 1 || sockets > 1000) {
  console.error("SLOWLORIS_SOCKETS must be an integer between 1 and 1000");
  process.exit(1);
}
if (!Number.isFinite(holdMs) || holdMs <= 0) {
  console.error("SLOWLORIS_HOLD_MS must be positive");
  process.exit(1);
}

function openSlowSocket() {
  return new Promise((resolve) => {
    const socket = net.createConnection({
      host: target.host,
      port: target.port,
    });
    let settled = false;
    const finish = () => {
      if (!settled) {
        settled = true;
        resolve(socket);
      }
    };
    socket.on("connect", () => {
      socket.write("GET /healthz HTTP/1.1\r\nHost: " + target.host + "\r\nX-Slow: ");
      finish();
    });
    socket.on("error", finish);
    setTimeout(finish, 1500);
  });
}

function healthCheck() {
  return new Promise((resolve) => {
    const req = http.get(new URL("/healthz", target.baseUrl), { timeout: 3000 }, (res) => {
      res.resume();
      resolve(res.statusCode);
    });
    req.on("timeout", () => req.destroy(new Error("health timeout")));
    req.on("error", () => resolve(0));
  });
}

const held = [];
for (let index = 0; index < sockets; index += 1) {
  held.push(await openSlowSocket());
}
const status = await healthCheck();
for (const socket of held) {
  socket.destroy();
}
if (status !== 200) {
  console.error(`slowloris: /healthz returned ${status} while ${sockets} slow sockets were open`);
  process.exit(1);
}
console.log(`slowloris: PASS held=${sockets} health=${status} hold_ms=${holdMs}`);