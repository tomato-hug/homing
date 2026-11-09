// Homing relay · scanner: claude
// 会话读取逻辑自 v1 server.mjs 原样搬迁（claudeSessionFiles / claudeFileMeta）。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { readJsonLines, firstLine, normalizeContent, readFileHeadAndTail, readFileSlice, NOISE_PATTERN, META_TTL_MS } from "./lib/util.mjs";

let ctx = {};                       // { transcriptPaths: Map, log }
// 缓存（v1 中的定义在抽取区块之前，这里补齐）
const claudeFilesCache = { at: 0, entries: [] };
const claudeMetaCache = new Map(); // path -> { size, mtimeMs, meta }

export function init(c) { ctx = c || {}; }

function claudeSessionFiles() {
  if (Date.now() - claudeFilesCache.at < META_TTL_MS && claudeFilesCache.entries.length) {
    return claudeFilesCache.entries;
  }
  const entries = [];
  const base = path.join(os.homedir(), ".claude", "projects");
  let dirs = [];
  try {
    dirs = fs.readdirSync(base, { withFileTypes: true });
  } catch {
    dirs = [];
  }
  for (const dir of dirs) {
    if (!dir.isDirectory()) {
      continue;
    }
    const dirPath = path.join(base, dir.name);
    let files = [];
    try {
      files = fs.readdirSync(dirPath);
    } catch {
      continue;
    }
    for (const file of files) {
      if (!file.endsWith(".jsonl")) {
        continue;
      }
      const full = path.join(dirPath, file);
      let stat;
      try {
        stat = fs.statSync(full);
      } catch {
        continue;
      }
      entries.push({
        path: full,
        sessionId: file.replace(/\.jsonl$/, ""),
        dirName: dir.name,
        size: stat.size,
        mtimeMs: stat.mtimeMs,
      });
    }
  }
  entries.sort((a, b) => b.mtimeMs - a.mtimeMs);
  claudeFilesCache.at = Date.now();
  claudeFilesCache.entries = entries;
  return entries;
}

function claudeFileMeta(entry) {
  const cached = claudeMetaCache.get(entry.path);
  if (cached && cached.size === entry.size && cached.mtimeMs === entry.mtimeMs) {
    return cached.meta;
  }
  const meta = {
    cwd: null,
    branch: null,
    model: null,
    effort: null,
    permissionMode: null,
    contextUsage: null,
    customTitle: null,
    aiTitle: null,
    summary: null,
    firstPrompt: null,
    lastTs: null,
    lastPreview: null,
  };
  const { head, tail } = readFileHeadAndTail(entry.path, entry.size);

  const apply = (line, fromHead) => {
    const trimmed = line.trim();
    if (!trimmed) {
      return;
    }
    let row;
    try {
      row = JSON.parse(trimmed);
    } catch {
      return;
    }
    if (!row || typeof row !== "object") {
      return;
    }
    if (typeof row.cwd === "string" && row.cwd) {
      meta.cwd = row.cwd;
    }
    if (typeof row.gitBranch === "string" && row.gitBranch) {
      meta.branch = row.gitBranch;
    }
    if (typeof row.timestamp === "string" && row.timestamp) {
      meta.lastTs = row.timestamp;
    }
    if (row.type === "custom-title" && typeof row.customTitle === "string") {
      meta.customTitle = row.customTitle;
    }
    if (row.type === "ai-title" && typeof row.aiTitle === "string") {
      meta.aiTitle = row.aiTitle;
    }
    if (row.type === "summary" && typeof row.summary === "string") {
      meta.summary = row.summary;
    }
    if (typeof row.permissionMode === "string" && row.permissionMode) {
      meta.permissionMode = row.permissionMode;
    }
    if (typeof row.effort === "string" && row.effort) {
      meta.effort = row.effort;
    } else if (typeof row.perTurnEffort === "string" && row.perTurnEffort) {
      meta.effort = row.perTurnEffort;
    }
    const message = row.message && typeof row.message === "object" ? row.message : null;
    if (message) {
      if (typeof message.model === "string" && message.model) {
        meta.model = message.model;
      }
      if (message.usage && typeof message.usage === "object") {
        const usage = message.usage;
        const used =
          (usage.input_tokens || 0) +
          (usage.cache_read_input_tokens || 0) +
          (usage.cache_creation_input_tokens || 0) +
          (usage.output_tokens || 0);
        if (used > 0) {
          meta.contextUsage = { used, limit: null };
        }
      }
      const text = normalizeContent(message.content).trim();
      const role = message.role || row.type;
      if (fromHead && !meta.firstPrompt && role === "user" && text && !NOISE_PATTERN.test(text)) {
        meta.firstPrompt = text;
      }
      if (!fromHead && role === "assistant" && text) {
        meta.lastPreview = firstLine(text, 120);
      }
    }
  };

  for (const line of head.split("\n")) {
    apply(line, true);
  }
  if (tail !== head) {
    for (const line of tail.split("\n")) {
      apply(line, false);
    }
  }

  claudeMetaCache.set(entry.path, { size: entry.size, mtimeMs: entry.mtimeMs, meta });
  return meta;
}

// ---- WorkBuddy ---------------------------------------------------------

// ---- Homing v2 接口 -------------------------------------------------------

const CLAUDE_HOME = () => path.join(os.homedir(), ".claude");

export async function detect() {
  const dir = CLAUDE_HOME();
  let version = null;
  try {
    const p = path.join(dir, "settings.json");
    if (fs.existsSync(p)) {
      const j = JSON.parse(fs.readFileSync(p, "utf8"));
      version = j.version || null;
    }
  } catch { /* 忽略 */ }
  return { installed: fs.existsSync(path.join(dir, "projects")), version };
}

export function meta(sessionId, cwdHint) {
  const out = { cwd: cwdHint || null, nativeTitle: "", titleSource: "agent", firstPrompt: "", branch: null, model: null };
  try {
    const entry = claudeSessionFiles().find((item) => item.sessionId === sessionId);
    const m = entry ? claudeFileMeta(entry) : null;
    if (m) {
      out.cwd = out.cwd || m.cwd;
      out.branch = m.branch;
      out.model = m.model;
      out.effort = m.effort;
      out.permissionMode = m.permissionMode;
      out.contextUsage = m.contextUsage;
      out.firstPrompt = m.firstPrompt || "";
      if (m.customTitle) { out.nativeTitle = m.customTitle; out.titleSource = "custom"; }
      else if (m.aiTitle) out.nativeTitle = m.aiTitle;
      else if (m.summary) out.nativeTitle = m.summary;
    }
    // 钩子载荷里的 transcript_path 是最准的
    const tp = typeof ctx.transcriptPaths === "function" ? ctx.transcriptPaths() : ctx.transcriptPaths;
    const recorded = tp?.get?.(sessionId);
    if (recorded?.path) {
      const stat = fs.statSync(recorded.path);
      const m2 = claudeFileMeta({ path: recorded.path, sessionId, size: stat.size, mtimeMs: stat.mtimeMs });
      out.cwd = out.cwd || m2.cwd;
      out.branch = m2.branch || out.branch;
      out.model = m2.model || out.model;
      out.effort = out.effort || m2.effort;
      out.permissionMode = out.permissionMode || m2.permissionMode;
      out.contextUsage = out.contextUsage || m2.contextUsage;
      out.firstPrompt = out.firstPrompt || m2.firstPrompt || "";
      if (!out.nativeTitle) {
        out.nativeTitle = m2.customTitle || m2.aiTitle || m2.summary || "";
        out.titleSource = m2.customTitle ? "custom" : "agent";
      }
    }
  } catch { /* 降级 */ }
  return out;
}

export async function listSessions({ limit = 200 } = {}) {
  let files = [];
  try { files = claudeSessionFiles(); } catch { files = []; }
  const rows = files
    .slice()
    .sort((a, b) => (b.mtimeMs || 0) - (a.mtimeMs || 0))
    .slice(0, limit)
    .map((e) => {
      let title = "";
      let source = "first_prompt";
      try {
        const m = claudeFileMeta(e);
        if (m.customTitle) { title = m.customTitle; source = "custom"; }
        else if (m.aiTitle) { title = m.aiTitle; source = "agent"; }
        else if (m.summary) { title = m.summary; source = "agent"; }
      } catch { /* 降级 */ }
      return {
        session_id: e.sessionId,
        source: "claude",
        title,
        title_source: source,
        last_ts: e.mtimeMs ? new Date(e.mtimeMs).toISOString() : null,
        native_state: null,
      };
    });
  return rows;
}

export const pushSignal = "hook";
export const capabilities = { reply: true, sessions: true };

export default {
  id: "claude", name: "Claude Code", icon: "claude",
  pushSignal, capabilities, init, detect, meta, listSessions,
  // 供 workbuddy 复用（两者转录同族）
  fileMeta: claudeFileMeta,
  files: claudeSessionFiles,
};
