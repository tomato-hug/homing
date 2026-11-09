#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { request } from "node:http";

const rawJson = process.argv[2];
const relayUrl = "http://127.0.0.1:18787/hook/codex";

function postToRelay() {
  if (!rawJson) {
    return;
  }

  try {
    const body = Buffer.from(rawJson, "utf8");
    const req = request(
      relayUrl,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": body.length,
        },
        timeout: 2000,
      },
      (response) => {
        response.resume();
      },
    );

    req.on("timeout", () => req.destroy());
    req.on("error", () => {});
    req.end(body);
  } catch {
    // Notification delivery must never break the calling agent.
  }
}

function chainNotify() {
  const command = process.env.CHAIN_NOTIFY;
  if (!command || !rawJson) {
    return;
  }

  try {
    const quotedJson = `'${rawJson.replaceAll("'", `'\\''`)}'`;
    spawnSync("/bin/sh", ["-c", `${command} ${quotedJson}`], {
      stdio: "ignore",
      timeout: 2000,
    });
  } catch {
    // Preserve the original notify command without making failures visible.
  }
}

postToRelay();
chainNotify();
process.exitCode = 0;
