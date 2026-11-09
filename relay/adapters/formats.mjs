// Homing relay · 记录格式预设
// 参照 magpie 的做法：少数几个格式覆盖大量 agent（它的 parserFor 里 default 直接落 Claude 格式）。
// 清单里 sessions.format 引用的就是这里的名字；custom 则由清单自己给字段映射。
//
// 每个格式提供三个方法：
//   listFiles(files, fs)   → [{ session_id, title, title_source, cwd, model, branch, last_ts, path }]
//   readItems(filePath, fs, {limit}) → [{ role, text, at, kind }]
//   hasConversation(filePath, fs)    → boolean（用于 is_empty）

import zlib from "node:zlib";
import { firstLine } from "../scanners/lib/util.mjs";

const MAX_TEXT = 4000;

function toIso(v) {
  if (v == null) return null;
  if (typeof v === "number") return new Date(v < 1e12 ? v * 1000 : v).toISOString();
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

function textOf(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((b) => (typeof b === "string" ? b : b?.text || b?.content || ""))
      .join("")
      .slice(0, MAX_TEXT);
  }
  if (content && typeof content === "object") return textOf(content.text ?? content.content ?? "");
  return "";
}

function readLines(filePath, fs) {
  try {
    // .zstd 是 DeepSeek Harness 的会话格式：Node 22+ 自带 zstd 解压
    if (/\.zstd?$/i.test(filePath)) {
      const buf = fs.readFileSync(filePath);
      const out = typeof zlib.zstdDecompressSync === "function"
        ? zlib.zstdDecompressSync(buf).toString("utf8")
        : "";
      return out.split("\n").filter((l) => l.trim());
    }
    return fs.readFileSync(filePath, "utf8").split("\n").filter((l) => l.trim());
  } catch {
    return [];
  }
}

function parseJsonLine(line) {
  try { return JSON.parse(line); } catch { return null; }
}

// ---- claude-code --------------------------------------------------------
// 一行一条记录：type/role/timestamp/sessionId/cwd/gitBranch/message.content；
// 标题来自 type:"custom-title" | "ai-title" | "summary"。

const claudeRole = (row) => {
  const role = String(row?.role || "").toLowerCase();
  if (role === "user" || role === "assistant") return role;
  const type = String(row?.type || "").toLowerCase();
  if (type === "user" || type === "assistant") return type;
  return null;
};

export const claudeCode = {
  name: "claude-code",
  ext: [".jsonl"],
  listFiles(files, fs) {
    return files.map((file) => {
      const out = { session_id: null, title: "", title_source: "first_prompt", cwd: null, model: null, branch: null, last_ts: null, path: file };
      let firstPrompt = "";
      for (const line of readLines(file, fs)) {
        const row = parseJsonLine(line);
        if (!row) continue;
        if (!out.session_id && row.sessionId) out.session_id = row.sessionId;
        if (!out.cwd && typeof row.cwd === "string") out.cwd = row.cwd;
        if (!out.branch && typeof row.gitBranch === "string" && row.gitBranch !== "HEAD") out.branch = row.gitBranch;
        if (!out.model && typeof row.message?.model === "string") out.model = row.message.model;
        if (row.timestamp) out.last_ts = toIso(row.timestamp);
        if (row.type === "custom-title" && row.customTitle) { out.title = row.customTitle; out.title_source = "custom"; }
        else if (row.type === "ai-title" && row.aiTitle) { if (out.title_source !== "custom") { out.title = row.aiTitle; out.title_source = "agent"; } }
        else if (row.type === "summary" && row.summary) { if (!out.title) { out.title = row.summary; out.title_source = "agent"; } }
        if (!firstPrompt && claudeRole(row) === "user") {
          const t = textOf(row.message?.content ?? row.content);
          if (t) firstPrompt = t;
        }
      }
      if (!out.session_id) {
        const m = /([0-9a-zA-Z-]{16,})\.jsonl$/.exec(file);
        if (m) out.session_id = m[1];
      }
      if (!out.title && firstPrompt) { out.title = firstLine(firstPrompt, 80); out.title_source = "first_prompt"; }
      return out;
    }).filter((r) => r.session_id);
  },
  readItems(filePath, fs, { limit = 100 } = {}) {
    const items = [];
    for (const line of readLines(filePath, fs)) {
      const row = parseJsonLine(line);
      if (!row) continue;
      const role = claudeRole(row);
      if (!role) continue;
      const text = textOf(row.message?.content ?? row.content);
      if (!text) continue;
      items.push({ role, kind: "message", text: text.slice(0, MAX_TEXT), at: toIso(row.timestamp) });
    }
    return items.slice(-limit);
  },
  hasConversation(filePath, fs) {
    for (const line of readLines(filePath, fs)) {
      const row = parseJsonLine(line);
      if (!row) continue;
      const role = claudeRole(row);
      if (role && textOf(row.message?.content ?? row.content)) return true;
    }
    return false;
  },
};

// ---- codex --------------------------------------------------------------
// 首行 session_meta.payload：{ id, cwd, git{ branch } }；其余 response_item/message。

export const codex = {
  name: "codex",
  ext: [".jsonl"],
  listFiles(files, fs) {
    return files.map((file) => {
      const out = { session_id: null, title: "", title_source: "first_prompt", cwd: null, model: null, branch: null, last_ts: null, path: file };
      let firstPrompt = "";
      for (const line of readLines(file, fs)) {
        const row = parseJsonLine(line);
        if (!row) continue;
        if (row.type === "session_meta" && row.payload) {
          out.session_id = out.session_id || row.payload.id || null;
          out.cwd = out.cwd || row.payload.cwd || null;
          out.branch = out.branch || row.payload?.git?.branch || null;
          if (row.timestamp) out.last_ts = toIso(row.timestamp);
        }
        if (row.timestamp) out.last_ts = toIso(row.timestamp);
        const p = row.payload;
        if (p?.type === "message" && p.role === "user" && !firstPrompt) {
          firstPrompt = textOf(p.content);
        }
        if (p?.type === "turn_context" && p.model) out.model = p.model;
      }
      if (!out.session_id) {
        const m = /([0-9a-f-]{36})\.jsonl$/.exec(file);
        if (m) out.session_id = m[1];
      }
      if (firstPrompt) { out.title = firstLine(firstPrompt, 80); }
      return out;
    }).filter((r) => r.session_id);
  },
  readItems(filePath, fs, { limit = 100 } = {}) {
    const items = [];
    for (const line of readLines(filePath, fs)) {
      const row = parseJsonLine(line);
      const p = row?.payload;
      if (p?.type !== "message") continue;
      const text = textOf(p.content);
      if (!text) continue;
      items.push({ role: p.role, kind: "message", text: text.slice(0, MAX_TEXT), at: toIso(row.timestamp) });
    }
    return items.slice(-limit);
  },
  hasConversation(filePath, fs) {
    return codex.readItems(filePath, fs, { limit: 5 }).length > 0;
  },
};

// ---- gemini-cli ---------------------------------------------------------
// 一个会话一个 JSON 文件：{ sessionId, messages: [{ type: "user" | "gemini", content }] }

export const geminiCli = {
  name: "gemini-cli",
  ext: [".json"],
  listFiles(files, fs) {
    return files.map((file) => {
      let doc = null;
      try { doc = JSON.parse(fs.readFileSync(file, "utf8")); } catch { return null; }
      const msgs = Array.isArray(doc?.messages) ? doc.messages : [];
      const firstUser = msgs.find((m) => /user/i.test(m?.type || m?.role || ""));
      const last = msgs[msgs.length - 1];
      return {
        session_id: doc?.sessionId || doc?.id || file.replace(/^.*\//, "").replace(/\.json$/, ""),
        title: firstLine(textOf(firstUser?.content), 80),
        title_source: "first_prompt",
        cwd: doc?.cwd || null,
        model: doc?.model || null,
        branch: null,
        last_ts: toIso(doc?.lastUpdated || doc?.updatedAt || last?.timestamp || null),
        path: file,
      };
    }).filter(Boolean);
  },
  readItems(filePath, fs, { limit = 100 } = {}) {
    let doc = null;
    try { doc = JSON.parse(fs.readFileSync(filePath, "utf8")); } catch { return []; }
    const msgs = Array.isArray(doc?.messages) ? doc.messages : [];
    return msgs
      .map((m) => {
        const t = String(m?.type || m?.role || "");
        const role = /user/i.test(t) ? "user" : "assistant";
        const text = textOf(m?.content);
        if (!text) return null;
        return { role, kind: "message", text: text.slice(0, MAX_TEXT), at: toIso(m?.timestamp) };
      })
      .filter(Boolean)
      .slice(-limit);
  },
  hasConversation(filePath, fs) {
    return geminiCli.readItems(filePath, fs, { limit: 5 }).length > 0;
  },
};

// ---- generic-jsonl ------------------------------------------------------
// 给"一行一条 JSON、字段名不一样"的 agent 用；字段映射由清单给。
// 映射：{ role, text, time, title, cwd, session_id, user_values: [...], assistant_values: [...] }

export const genericJsonl = {
  name: "generic-jsonl",
  ext: [".jsonl"],
  make(mapping = {}) {
    const pick = (obj, keyPath) => {
      if (!keyPath) return undefined;
      return String(keyPath).split(".").reduce((acc, k) => (acc == null ? acc : acc[k]), obj);
    };
    const roleOf = (row) => {
      const v = pick(row, mapping.role);
      const s = String(v ?? "").toLowerCase();
      const users = (mapping.user_values || ["user"]).map((x) => String(x).toLowerCase());
      const assist = (mapping.assistant_values || ["assistant", "agent", "gemini", "model"]).map((x) => String(x).toLowerCase());
      if (users.includes(s)) return "user";
      if (assist.includes(s)) return "assistant";
      return null;
    };
    return {
      name: "custom",
      ext: [".jsonl"],
      listFiles(files, fs) {
        return files.map((file) => {
          const out = { session_id: null, title: "", title_source: "first_prompt", cwd: null, model: null, branch: null, last_ts: null, path: file };
          let firstPrompt = "";
          for (const line of readLines(file, fs)) {
            const row = parseJsonLine(line);
            if (!row) continue;
            if (!out.session_id) out.session_id = pick(row, mapping.session_id) ?? null;
            if (!out.cwd) out.cwd = pick(row, mapping.cwd) ?? null;
            if (!out.model) out.model = pick(row, mapping.model) ?? null;
            const t = pick(row, mapping.time);
            if (t != null) out.last_ts = toIso(t);
            const title = pick(row, mapping.title);
            if (title && !out.title) { out.title = String(title); out.title_source = "agent"; }
            if (!firstPrompt && roleOf(row) === "user") {
              firstPrompt = textOf(pick(row, mapping.text));
            }
          }
          if (!out.session_id) {
            const m = /([0-9a-zA-Z-]{8,})\.jsonl$/.exec(file);
            if (m) out.session_id = m[1];
          }
          if (!out.title && firstPrompt) out.title = firstLine(firstPrompt, 80);
          return out;
        }).filter((r) => r.session_id);
      },
      readItems(filePath, fs, { limit = 100 } = {}) {
        const items = [];
        for (const line of readLines(filePath, fs)) {
          const row = parseJsonLine(line);
          if (!row) continue;
          const role = roleOf(row);
          if (!role) continue;
          const text = textOf(pick(row, mapping.text));
          if (!text) continue;
          items.push({ role, kind: "message", text: text.slice(0, MAX_TEXT), at: toIso(pick(row, mapping.time)) });
        }
        return items.slice(-limit);
      },
      hasConversation(filePath, fs) {
        return this.readItems(filePath, fs, { limit: 5 }).length > 0;
      },
    };
  },
};

export const FORMATS = {
  "claude-code": () => claudeCode,
  "codex": () => codex,
  "gemini-cli": () => geminiCli,
  "generic-jsonl": (mapping) => genericJsonl.make(mapping || {}),
  "custom": (mapping) => genericJsonl.make(mapping || {}),
};

export function formatFor(name, mapping) {
  const make = FORMATS[name];
  if (!make) return null;
  return make(mapping);
}

export const FORMAT_NAMES = Object.keys(FORMATS);
