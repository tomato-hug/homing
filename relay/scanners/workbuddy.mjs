// Homing relay · scanner: workbuddy
// 会话读取逻辑自 v1 server.mjs 原样搬迁（workbuddySnapshot）。
// 会话列表另有更省事的来源：~/.workbuddy/<uid>/sidebar-list-snapshot.json。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { readJsonLines, firstLine, normalizeContent, readFileHeadAndTail, readFileSlice, META_TTL_MS } from "./lib/util.mjs";

let ctx = {};                       // { transcriptPaths: Map, log }
const workbuddySnapshotCache = { key: "", map: new Map() };

export function init(c) { ctx = c || {}; }

function workbuddySnapshot() {
  const base = path.join(os.homedir(), ".workbuddy");
  let dirs;
  try {
    dirs = fs.readdirSync(base, { withFileTypes: true });
  } catch {
    return new Map();
  }
  for (const dir of dirs) {
    if (!dir.isDirectory()) {
      continue;
    }
    const file = path.join(base, dir.name, "sidebar-list-snapshot.json");
    let stat;
    try {
      stat = fs.statSync(file);
    } catch {
      continue;
    }
    const key = `${file}|${stat.mtimeMs}|${stat.size}`;
    if (workbuddySnapshotCache.key === key) {
      return workbuddySnapshotCache.map;
    }
    let data;
    try {
      data = JSON.parse(fs.readFileSync(file, "utf8"));
    } catch {
      continue;
    }
    const map = new Map();
    for (const item of data.items || []) {
      if (item && typeof item.id === "string") {
        map.set(item.id, item);
      }
    }
    workbuddySnapshotCache.key = key;
    workbuddySnapshotCache.map = map;
    return map;
  }
  return new Map();
}

// ---- 公共字段推导 -------------------------------------------------------

// ---- Homing v2 接口 -------------------------------------------------------

const WB_HOME = () => path.join(os.homedir(), ".workbuddy");

// 会话索引快照：一次读全，带 title / state / space.name
export function sidebarSnapshot() {
  const root = WB_HOME();
  if (!fs.existsSync(root)) return [];
  let best = null;
  for (const name of fs.readdirSync(root)) {
    const p = path.join(root, name, "sidebar-list-snapshot.json");
    if (!fs.existsSync(p)) continue;
    try {
      const stat = fs.statSync(p);
      if (!best || stat.mtimeMs > best.mtime) best = { p, mtime: stat.mtimeMs };
    } catch { /* 忽略 */ }
  }
  if (!best) return [];
  try {
    const j = JSON.parse(fs.readFileSync(best.p, "utf8"));
    return Array.isArray(j.items) ? j.items : [];
  } catch {
    return [];
  }
}

export async function detect() {
  const dir = WB_HOME();
  return { installed: fs.existsSync(dir), version: null };
}

export function meta(sessionId, cwdHint) {
  const out = { cwd: cwdHint || null, nativeTitle: "", titleSource: "agent", firstPrompt: "", branch: null, model: null, workspace: null };
  try {
    const item = workbuddySnapshot().get(sessionId);
    if (item) {
      out.nativeTitle = typeof item.title === "string" ? item.title : "";
      const space = item.space && typeof item.space === "object" ? item.space : null;
      if (space && typeof space.name === "string" && space.name) {
        out.cwd = out.cwd || space.name;
        out.workspace = space.name;
      }
      if (typeof item.state === "string") out.nativeState = item.state;
    }
    const tp = typeof ctx.transcriptPaths === "function" ? ctx.transcriptPaths() : ctx.transcriptPaths;
    const recorded = tp?.get?.(sessionId);
    if (recorded?.path) {
      const stat = fs.statSync(recorded.path);
      const m2 = claudeLikeMeta({ path: recorded.path, sessionId, size: stat.size, mtimeMs: stat.mtimeMs });
      out.cwd = out.cwd || m2.cwd;
      out.firstPrompt = out.firstPrompt || m2.firstPrompt || "";
      out.model = out.model || m2.model;
      if (!out.nativeTitle) {
        out.nativeTitle = m2.customTitle || m2.aiTitle || "";
        out.titleSource = m2.customTitle ? "custom" : "agent";
      }
    }
  } catch { /* 降级 */ }
  return out;
}

// WorkBuddy 的转录与 Claude 同族：借用 claude scanner 的解析器
import claudeScanner from "./claude.mjs";
function claudeLikeMeta(entry) {
  // claude.mjs 的 claudeFileMeta 未导出为公共方法，这里走它的内部实现
  return claudeScanner.fileMeta ? claudeScanner.fileMeta(entry) : {};
}

export async function listSessions({ limit = 200 } = {}) {
  const items = sidebarSnapshot();
  const rows = items.slice(0, limit).map((i) => ({
    session_id: i.id,
    source: "workbuddy",
    title: typeof i.title === "string" ? i.title : "",
    title_source: "agent",
    last_ts: typeof i.lastActivityAt === "number" ? new Date(i.lastActivityAt).toISOString() : null,
    native_state: typeof i.state === "string" ? i.state : null,
    cwd: i.space && typeof i.space.name === "string" ? i.space.name : null,
  }));
  rows.sort((a, b) => ((a.last_ts || "") < (b.last_ts || "") ? 1 : -1));
  return rows;
}

export const pushSignal = "hook";
export const capabilities = { reply: true, sessions: true };

export default {
  id: "workbuddy", name: "WorkBuddy", icon: "workbuddy",
  pushSignal, capabilities, init, detect, meta, listSessions, sidebarSnapshot,
};
