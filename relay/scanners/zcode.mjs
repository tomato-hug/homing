// Homing relay · scanner: zcode（智谱）
// 数据源：~/.zcode/cli/db/db.sqlite —— 与 OpenCode 同一套表（magpie 的 zcode.go 也共用 parseOpenCode）。
// 只读轮询；v2 不做回复（capabilities.reply=false）。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import * as oc from "./lib/opencode-db.mjs";

let ctx = {};
export function init(c) { ctx = c || {}; }

export function dbPath() {
  return path.join(os.homedir(), ".zcode", "cli", "db", "db.sqlite");
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
    source: "zcode",
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

export const pushSignal = "poll";
export const capabilities = { reply: false, sessions: true };

export default {
  id: "zcode",
  name: "ZCode",
  icon: "zcode",
  pushSignal,
  capabilities,
  init,
  detect,
  meta,
  listSessions,
  readItems,
  isEmptySession,
};
