// Homing relay · scanner: opencode
// 数据源：OpenCode 的 SQLite（默认 ~/.local/share/opencode/opencode.db，$OPENCODE_DB 可覆盖）。
// 只读打开，不写库、不加锁。检测到 `opencode serve` 时可改用它的 HTTP 事件（v2 暂用轮询）。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import * as oc from "./lib/opencode-db.mjs";

let ctx = {};                       // { log }
export function init(c) { ctx = c || {}; }

export function dbPath() {
  const override = process.env.OPENCODE_DB;
  const base = process.env.XDG_DATA_HOME
    ? path.join(process.env.XDG_DATA_HOME, "opencode")
    : path.join(os.homedir(), ".local", "share", "opencode");
  if (override && override !== ":memory:") {
    return path.isAbsolute(override) ? override : path.join(base, override);
  }
  return path.join(base, "opencode.db");
}

export async function detect() {
  const p = dbPath();
  const installed = fs.existsSync(p);
  const info = installed ? oc.probe(p) : { ok: false, sessions: 0 };
  return { installed, version: null, detail: info.ok ? `${info.sessions} sessions` : null };
}

export function meta(sessionId, cwdHint) {
  const row = oc.oneSession(dbPath(), sessionId);
  return {
    cwd: cwdHint || row?.cwd || null,
    nativeTitle: row?.title || "",
    titleSource: "agent",
    firstPrompt: "",
    branch: null,
    model: row?.model || null,
    nativeState: row?.archived_native ? "archived" : null,
  };
}

export async function listSessions({ limit = 200 } = {}) {
  const p = dbPath();
  if (!fs.existsSync(p)) return [];
  return oc.listSessions(p, limit).map((r) => ({
    session_id: r.session_id,
    source: "opencode",
    title: r.title || "",
    title_source: "agent",
    last_ts: r.last_ts,
    native_state: r.archived_native ? "archived" : null,
    cwd: r.cwd,
    model: r.model,
    parent_id: r.parent_id,
  }));
}

export function readItems(sessionId, { limit = 100 } = {}) {
  const p = dbPath();
  if (!fs.existsSync(p)) return [];
  return oc.readItems(p, sessionId, limit);
}

export function isEmptySession(sessionId) {
  const p = dbPath();
  if (!fs.existsSync(p)) return false;
  return !oc.hasConversation(p, sessionId);
}

export const pushSignal = "poll";     // 有 opencode serve 时可切 http_events
export const capabilities = { reply: false, sessions: true, reply_reason: "v2 暂未实现 OpenCode 注入" };

export default {
  id: "opencode",
  name: "OpenCode",
  icon: "opencode",
  pushSignal,
  capabilities,
  init,
  detect,
  meta,
  listSessions,
  readItems,
  isEmptySession,
};
