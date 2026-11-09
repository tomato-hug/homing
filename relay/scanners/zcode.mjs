// Homing relay · scanner: zcode（智谱 ZCode，OpenCode 的改版）
// 数据源：ZCode 自己的 SQLite（与 OpenCode 同表结构）。只读，不写不锁。
// 本机实测（2026-11-09）：**未安装**——~/.zcode、~/Library/Application Support/ZCode、
// Preferences、VSCode 扩展、Homebrew cask、mdfind 全部为空，因此 detect() 返回 missing。
// 检测保留多候选路径，装了之后无需改代码即可识别。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";
import * as oc from "./lib/opencode-db.mjs";

let ctx = {};
export function init(c) { ctx = c || {}; }

// ---- 候选路径 -----------------------------------------------------------
// magpie 的 zcode.go：~/.zcode/cli/db/db.sqlite（Windows 上即便 app 数据在别处，库也在 USERPROFILE）。
// macOS 上再补两个常见位置，供不同安装方式使用。

export function candidates() {
  const home = os.homedir();
  const roots = [];
  if (process.env.ZCODE_HOME) roots.push(process.env.ZCODE_HOME);
  roots.push(path.join(home, ".zcode"));
  roots.push(path.join(home, "Library", "Application Support", "ZCode"));
  roots.push(path.join(home, "Library", "Application Support", "zcode"));

  const out = [];
  for (const root of roots) {
    out.push(path.join(root, "cli", "db", "db.sqlite"));
    out.push(path.join(root, "db", "db.sqlite"));
    out.push(path.join(root, "db.sqlite"));
    out.push(path.join(root, "cli", "db", "zcode.db"));
  }
  // 去重
  return [...new Set(out)];
}

export function dbPath() {
  for (const p of candidates()) {
    try {
      if (fs.existsSync(p)) return p;
    } catch { /* 忽略 */ }
  }
  return null;
}

function configCandidates() {
  const home = os.homedir();
  const roots = [];
  if (process.env.ZCODE_HOME) roots.push(process.env.ZCODE_HOME);
  roots.push(path.join(home, ".zcode"));
  roots.push(path.join(home, ".config", "zcode"));
  return roots.flatMap((r) => [
    path.join(r, "cli", "config.json"),
    path.join(r, "config.json"),
    path.join(r, "opencode.json"),
  ]);
}

// ---- 检测 ---------------------------------------------------------------

export async function detect() {
  const p = dbPath();
  const info = p ? oc.probe(p) : { ok: false, sessions: 0 };
  const checked = candidates();
  return {
    installed: Boolean(p) || info.ok,
    version: null,
    detail: p
      ? `${info.sessions} sessions · ${p}`
      : `未找到数据库（查过 ${checked.length} 个路径）`,
    db: p,
    checked,
  };
}

// ---- 会话 ---------------------------------------------------------------

export function meta(sessionId, cwdHint) {
  const p = dbPath();
  if (!p) {
    return { cwd: cwdHint || null, nativeTitle: "", titleSource: "agent", firstPrompt: "", branch: null, model: null };
  }
  const row = oc.oneSession(p, sessionId);
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
  if (!p) return [];
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
  if (!p) return [];
  return oc.readItems(p, sessionId, limit);
}

export function isEmptySession(sessionId) {
  const p = dbPath();
  if (!p) return false;
  return !oc.hasConversation(p, sessionId);
}

// ---- 模型列表 -----------------------------------------------------------
// ZCode 与 OpenCode 同族：配置里 provider 的 models，缺失时回落到 models.dev 目录缓存。
// 本机没有安装，因此没有真实样本可核对，这里按同族格式读取；读不到就返回空（不臆造）。

export async function models() {
  const p = dbPath();
  const catalog = p ? catalogFromDB(p) : null;
  for (const cfgPath of configCandidates()) {
    try {
      if (!fs.existsSync(cfgPath)) continue;
      const cfg = JSON.parse(fs.readFileSync(cfgPath, "utf8"));
      const providers = cfg?.provider && typeof cfg.provider === "object" ? cfg.provider : {};
      const list = [];
      for (const [pid, prov] of Object.entries(providers)) {
        const own = prov?.models && typeof prov.models === "object" ? Object.keys(prov.models) : [];
        const known = own.length ? own : Object.keys(catalog?.[pid]?.models || {});
        for (const mid of known) list.push({ id: `${pid}/${mid}`, provider: pid, model: mid });
      }
      const def = typeof cfg.model === "string" ? cfg.model : null;
      if (list.length || def) {
        return { models: list, defaultModel: def, source: "config" };
      }
    } catch {
      // 试下一个候选
    }
  }
  return { models: [], defaultModel: null, source: "none" };
}

function catalogFromDB(p) {
  try {
    const db = new DatabaseSync(p, { readOnly: true });
    try {
      const row = db.prepare("select value from kv where key='models-dev:catalog'").get();
      if (!row?.value) return null;
      const outer = JSON.parse(row.value);
      return outer?.body ? JSON.parse(outer.body) : null;
    } finally {
      db.close();
    }
  } catch {
    return null;
  }
}

// ---- 注册 ---------------------------------------------------------------

export default {
  id: "zcode",
  name: "ZCode",
  icon: "zcode",
  init,
  detect,
  meta,
  listSessions,
  readItems,
  isEmptySession,
  models,
  pushSignal: "poll",
  capabilities: { reply: false, sessions: true },
};
