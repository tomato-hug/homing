// Homing relay · scanner: codex
// 会话读取逻辑自 v1 server.mjs 原样搬迁（codexTitles / codexRolloutIndex / codexRolloutMeta / findCodexRollout）。
// 新增：detect / listSessions（扫描原生 rollout 文件）/ meta（供 server 分派）。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { readJsonLines, firstLine, normalizeContent, readFileHeadAndTail, readFileSlice, gitBranchFor, workspaceLabel, META_TTL_MS } from "./lib/util.mjs";

let ctx = {};                       // { injectReply, log }
// 标题索引缓存（v1 中的定义在被截取的 codex 区块之前，这里补齐）
const codexTitleIndex = { key: "", map: new Map() };

export function init(c) { ctx = c || {}; }

function codexTitles() {
  const file = path.join(os.homedir(), ".codex", "session_index.jsonl");
  let stat;
  try {
    stat = fs.statSync(file);
  } catch {
    return new Map();
  }
  const key = `${stat.mtimeMs}|${stat.size}`;
  if (codexTitleIndex.key === key) {
    return codexTitleIndex.map;
  }
  const map = new Map();
  for (const row of readJsonLines(file)) {
    if (!row || typeof row.id !== "string") {
      continue;
    }
    const updated = typeof row.updated_at === "string" ? row.updated_at : "";
    const previous = map.get(row.id);
    if (!previous || updated >= previous.updated_at) {
      map.set(row.id, {
        thread_name: typeof row.thread_name === "string" ? row.thread_name : "",
        updated_at: updated,
      });
    }
  }
  codexTitleIndex.key = key;
  codexTitleIndex.map = map;
  return map;
}

function codexRolloutIndex() {
  if (Date.now() - codexRolloutIndexCache.at < META_TTL_MS && codexRolloutIndexCache.map.size) {
    return codexRolloutIndexCache.map;
  }
  const map = new Map();
  const base = path.join(os.homedir(), ".codex", "sessions");
  const walk = (dir, depth) => {
    if (depth > 4) {
      return;
    }
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full, depth + 1);
        continue;
      }
      if (!entry.name.startsWith("rollout-") || !entry.name.endsWith(".jsonl")) {
        continue;
      }
      const match = /-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$/i.exec(
        entry.name,
      );
      if (match) {
        map.set(match[1], full);
      }
    }
  };
  walk(base, 0);
  codexRolloutIndexCache.at = Date.now();
  codexRolloutIndexCache.map = map;
  return map;
}

function codexRolloutMeta(filePath) {
  let stat;
  try {
    stat = fs.statSync(filePath);
  } catch {
    return null;
  }
  const cached = codexRolloutMetaCache.get(filePath);
  if (cached && cached.size === stat.size && cached.mtimeMs === stat.mtimeMs) {
    return cached.meta;
  }

  const meta = {
    cwd: null,
    branch: null,
    model: null,
    effort: null,
    permissionMode: null,
    contextUsage: null,
    originator: null,
    lastTs: null,
    lastPreview: null,
    firstPrompt: null,
  };
  const { head, tail } = readFileHeadAndTail(filePath, stat.size);

  for (const line of head.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    let row;
    try {
      row = JSON.parse(trimmed);
    } catch {
      continue;
    }
    const payload = row?.payload && typeof row.payload === "object" ? row.payload : null;
    if (!payload) {
      continue;
    }
    if (row.type === "session_meta") {
      if (typeof payload.cwd === "string") {
        meta.cwd = payload.cwd;
      }
      if (payload.git && typeof payload.git.branch === "string") {
        meta.branch = payload.git.branch;
      }
      if (typeof payload.originator === "string") {
        meta.originator = payload.originator;
      }
    }
    if (
      !meta.firstPrompt &&
      payload.type === "message" &&
      payload.role === "user"
    ) {
      const text = normalizeContent(payload.content).trim();
      if (text && !NOISE_PATTERN.test(text)) {
        meta.firstPrompt = text;
      }
    }
  }

  for (const line of tail.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    let row;
    try {
      row = JSON.parse(trimmed);
    } catch {
      continue;
    }
    if (typeof row.timestamp === "string") {
      meta.lastTs = row.timestamp;
    }
    const payload = row?.payload && typeof row.payload === "object" ? row.payload : null;
    if (!payload) {
      continue;
    }
    if (typeof payload.model === "string" && payload.model) {
      meta.model = payload.model;
    }
    if (typeof payload.effort === "string" && payload.effort) {
      meta.effort = payload.effort;
    }
    if (typeof payload.approval_policy === "string" && payload.approval_policy) {
      meta.permissionMode = payload.approval_policy;
    } else if (payload.sandbox_policy?.type) {
      meta.permissionMode = payload.sandbox_policy.type;
    } else if (payload.permission_profile?.type) {
      meta.permissionMode = payload.permission_profile.type;
    }
    if (payload.type === "token_count" && payload.info) {
      const usage = payload.info.last_token_usage || payload.info.total_token_usage || null;
      if (usage) {
        const used =
          typeof usage.total_tokens === "number"
            ? usage.total_tokens
            : (usage.input_tokens || 0) + (usage.cached_input_tokens || 0) + (usage.output_tokens || 0);
        meta.contextUsage = {
          used,
          limit: typeof payload.info.model_context_window === "number" ? payload.info.model_context_window : null,
        };
      }
    }
    if (
      payload.thread_settings &&
      typeof payload.thread_settings.model === "string" &&
      payload.thread_settings.model
    ) {
      meta.model = payload.thread_settings.model;
    }
    if (payload.type === "message" && payload.role === "assistant") {
      const text = normalizeContent(payload.content).trim();
      if (text) {
        meta.lastPreview = firstLine(text, 120);
      }
    }
    const item = payload.item;
    if (item && item.type === "agent_message" && typeof item.text === "string" && item.text.trim()) {
      meta.lastPreview = firstLine(item.text, 120);
    }
  }

  codexRolloutMetaCache.set(filePath, { size: stat.size, mtimeMs: stat.mtimeMs, meta });
  return meta;
}

// ---- Claude Code -------------------------------------------------------

// ---- Homing v2 接口 -------------------------------------------------------

const CODEX_HOME = () => path.join(os.homedir(), ".codex");
function sessionsRoot() { return path.join(CODEX_HOME(), "sessions"); }

// 最近 N 天的 rollout 文件（按目录倒序遍历，够用即停）
function walkRollouts(limitDays = 45) {
  const root = sessionsRoot();
  const out = [];
  if (!fs.existsSync(root)) return out;
  const days = [];
  for (const y of fs.readdirSync(root)) {
    if (!/^\d{4}$/.test(y)) continue;
    for (const m of fs.readdirSync(path.join(root, y))) {
      for (const d of fs.readdirSync(path.join(root, y, m))) {
        days.push({ key: `${y}-${m}-${d}`, dir: path.join(root, y, m, d) });
      }
    }
  }
  days.sort((a, b) => (a.key < b.key ? 1 : -1));
  for (const day of days.slice(0, limitDays)) {
    for (const f of fs.readdirSync(day.dir)) {
      if (f.startsWith("rollout-") && f.endsWith(".jsonl")) {
        out.push(path.join(day.dir, f));
      }
    }
  }
  return out;
}

export async function detect() {
  const dir = CODEX_HOME();
  const installed = fs.existsSync(dir);
  let version = null;
  try {
    const p = path.join(dir, "version.json");
    if (fs.existsSync(p)) {
      const v = JSON.parse(fs.readFileSync(p, "utf8"));
      version = v.version || v.latest_version || null;
    }
  } catch { /* 版本读不到不影响 */ }
  return { installed, version };
}

// 会话元数据：v1 sessionMetaFor 的 codex 分支，原样搬到这里
export function meta(sessionId, cwdHint) {
  const out = { cwd: cwdHint || null, nativeTitle: "", titleSource: "agent", firstPrompt: "", branch: null, model: null };
  try {
    const index = codexTitles().get(sessionId);
    if (index?.thread_name) out.nativeTitle = index.thread_name;
    const rolloutPath = codexRolloutIndex().get(sessionId) || findCodexRollout(sessionId);
    const m = rolloutPath ? codexRolloutMeta(rolloutPath) : null;
    if (m) {
      out.cwd = out.cwd || m.cwd;
      out.branch = m.branch;
      out.model = m.model;
      out.firstPrompt = m.firstPrompt || "";
      out.permissionMode = m.permissionMode;
      out.contextUsage = m.contextUsage;
      out.effort = m.effort;
    }
  } catch { /* 降级 */ }
  return out;
}

// 原生会话发现：没有事件也能列出来
export async function listSessions({ limit = 200 } = {}) {
  const titles = codexTitles();
  const seen = new Set();
  const rows = [];
  for (const file of walkRollouts()) {
    let stat;
    try { stat = fs.statSync(file); } catch { continue; }
    let id = null;
    try {
      const headTxt = readFileSlice(file, 0, 64 * 1024);
      const first = headTxt.split("\n").find((l) => l.includes('"session_meta"'));
      if (first) {
        const row = JSON.parse(first);
        id = row?.payload?.id || null;
      }
    } catch { /* 头部解析失败就用文件名 */ }
    if (!id) {
      const m = /([0-9a-f-]{36})\.jsonl$/.exec(file);
      id = m ? m[1] : null;
    }
    if (!id || seen.has(id)) continue;
    seen.add(id);
    rows.push({ session_id: id, last_ts: new Date(stat.mtimeMs).toISOString(), path: file, size: stat.size });
  }
  rows.sort((a, b) => (a.last_ts < b.last_ts ? 1 : -1));
  return rows.slice(0, limit).map((r) => {
    const t = titles.get(r.session_id);
    return {
      session_id: r.session_id,
      source: "codex",
      title: t?.thread_name || "",
      title_source: t?.thread_name ? "agent" : "first_prompt",
      last_ts: r.last_ts,
      native_state: null,
    };
  });
}

export const pushSignal = "hook";
export const capabilities = { reply: true, sessions: true };

export default {
  id: "codex",
  name: "Codex",
  icon: "codex",
  pushSignal,
  capabilities,
  init,
  detect,
  meta,
  listSessions,
};
