#!/usr/bin/env node
// 通用 Homing 钩子（Node 版）：stdin 收一个 JSON 对象，补上常见 agent 的
// 字段别名后 POST 到本机 /hook/generic。任何失败都退出 0，避免通知故障
// 反过来阻塞 agent。
//
// 用法：
//   echo '{"session_id":"demo-1","text":"完成"}' | \
//     HOMING_AGENT=my-agent HOMING_KIND=done node homing-hook.mjs

import { request } from "node:http";

function arg(name) {
  const prefix = `--${name}=`;
  const hit = process.argv.slice(2).find((value) => value.startsWith(prefix));
  return hit ? hit.slice(prefix.length) : "";
}

function firstString(source, keys) {
  for (const key of keys) {
    if (typeof source?.[key] === "string" && source[key].trim()) return source[key];
  }
  return "";
}

function kindFromEvent(body) {
  const event = firstString(body, ["hook_event_name", "event", "type"]).toLowerCase();
  switch (event) {
    case "stop":
    case "agent-turn-complete":
    case "turn-complete":
    case "turn_complete":
      return "reply";
    case "permissionrequest":
    case "permission_request":
    case "approval":
    case "approval_request":
      return "permission";
    case "notification":
    case "needsinput":
    case "needs_input":
    case "needsreply":
    case "needs_reply":
      return "needs_input";
    case "error":
    case "failed":
    case "failure":
      return "error";
    case "done":
    case "completed":
    case "complete":
      return "done";
    default:
      return "";
  }
}

function normalize(raw, source, kind) {
  const body = JSON.parse(raw);
  if (!body || typeof body !== "object" || Array.isArray(body)) return raw;

  if (!body.source && !body.agent && source) body.source = source;
  if (!body.kind) {
    body.kind = kind || kindFromEvent(body) || "message";
  }
  if (!body.session_id) {
    body.session_id =
      firstString(body, [
        "sessionId",
        "thread_id",
        "thread-id",
        "conversation_id",
        "conversationId",
        "id",
      ]) ||
      process.env.HOMING_SESSION_ID ||
      "";
  }
  if (!body.cwd) {
    body.cwd = firstString(body, ["workdir", "working_directory", "project_dir"]);
  }
  if (!body.title) {
    body.title = firstString(body, ["name"]);
  }
  if (!body.text) {
    body.text = firstString(body, [
      "message",
      "last_assistant_message",
      "last-assistant-message",
      "output",
    ]);
  }
  return JSON.stringify(body);
}

const source = arg("source") || process.env.HOMING_AGENT || "";
const kind = arg("kind") || process.env.HOMING_KIND || "";
const url = new URL(
  arg("url") || process.env.HOMING_HOOK_URL || "http://127.0.0.1:18787/hook/generic",
);

let raw = "";
for await (const chunk of process.stdin) raw += chunk;
if (!raw.trim()) process.exit(0);

let payload;
try {
  payload = normalize(raw, source, kind);
} catch {
  process.exit(0);
}

try {
  const body = Buffer.from(payload, "utf8");
  const target = request(
    {
      protocol: url.protocol,
      hostname: url.hostname,
      port: url.port || 80,
      path: `${url.pathname}${url.search}`,
      method: "POST",
      timeout: 2000,
      headers: {
        "Content-Type": "application/json",
        "Content-Length": body.length,
        ...(source ? { "X-Homing-Agent": source } : {}),
        ...(kind ? { "X-Homing-Kind": kind } : {}),
      },
    },
    (response) => response.resume(),
  );
  target.on("timeout", () => target.destroy());
  target.on("error", () => {});
  target.end(body);
} catch {
  // 静默
}

process.exitCode = 0;
