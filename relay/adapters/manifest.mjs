// Homing relay · 适配器清单（~/.homing/agents/*.yaml）
// 清单在加载时被转换成与内置 agent 完全相同的 Scanner，因此列表、详情、收件箱不需要区分来源。
// 平台目录：macOS/Linux ~/.homing/agents，Windows %APPDATA%\Homing\agents。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { parseYaml, expandHome } from "./yaml.mjs";
import { formatFor, FORMAT_NAMES } from "./formats.mjs";
import { DatabaseSync } from "node:sqlite";

const HOME = os.homedir();

export function manifestDir() {
  if (process.platform === "win32") {
    const appdata = process.env.APPDATA || path.join(HOME, "AppData", "Roaming");
    return path.join(appdata, "Homing", "agents");
  }
  return path.join(HOME, ".homing", "agents");
}

// ---- glob（只支持 * 与 **，够清单用）------------------------------------

function globToRegex(pattern) {
  let re = "^";
  for (let i = 0; i < pattern.length; i++) {
    const ch = pattern[i];
    if (ch === "*") {
      if (pattern[i + 1] === "*") {
        re += ".*";
        i += 1;
        if (pattern[i + 1] === "/") i += 1;
      } else {
        re += "[^/]*";
      }
    } else if (ch === "?") {
      re += "[^/]";
    } else if ("\\^$.|+()[]{}".includes(ch)) {
      re += `\\${ch}`;
    } else {
      re += ch;
    }
  }
  return new RegExp(`${re}$`);
}

function walk(dir, out, depth = 0) {
  if (depth > 8) return;
  let entries;
  try { entries = fs.readdirSync(dir, { withFileTypes: true }); } catch { return; }
  for (const e of entries) {
    const full = path.join(dir, e.name);
    if (e.isDirectory()) walk(full, out, depth + 1);
    else out.push(full);
  }
}

// 找出 glob 的静态前缀目录，避免从 / 开始遍历
function staticRoot(pattern) {
  const idx = pattern.search(/[*?]/);
  if (idx < 0) return path.dirname(pattern);
  const head = pattern.slice(0, idx);
  const cut = head.lastIndexOf("/");
  return cut <= 0 ? "/" : head.slice(0, cut);
}

export function expandGlob(pattern) {
  const abs = expandHome(pattern, HOME);
  const root = staticRoot(abs);
  if (!fs.existsSync(root)) return [];
  const all = [];
  walk(root, all);
  const re = globToRegex(abs);
  return all.filter((f) => re.test(f)).sort((a, b) => {
    try { return fs.statSync(b).mtimeMs - fs.statSync(a).mtimeMs; } catch { return 0; }
  });
}

// ---- 校验 ---------------------------------------------------------------

export class ManifestError extends Error {
  constructor(message, field) {
    super(message);
    this.name = "ManifestError";
    this.field = field;
  }
}

export function validateManifest(raw, file = null) {
  const errors = [];
  const m = raw && typeof raw === "object" ? raw : {};
  const id = String(m.id ?? "").trim();
  if (!id) errors.push("缺少 id");
  else if (!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(id)) errors.push("id 只能用小写字母、数字、点、下划线、连字符（≤64）");

  const name = String(m.name ?? id);
  const icon = String(m.icon ?? "generic");

  const detect = m.detect && typeof m.detect === "object" ? m.detect : {};
  const detectPaths = []
    .concat(detect.paths || [], detect.path || [])
    .filter(Boolean)
    .map((p) => expandHome(String(p), HOME));
  const detectBins = [].concat(detect.bin || [], detect.bins || []).filter(Boolean).map(String);

  const s = m.sessions && typeof m.sessions === "object" ? m.sessions : null;
  if (!s) errors.push("缺少 sessions");
  const source = String(s?.source ?? "");
  if (s && !["jsonl", "json", "sqlite", "http"].includes(source)) {
    errors.push(`sessions.source 必须是 jsonl | json | sqlite | http（收到 ${source || "空"}）`);
  }
  const formatName = String(s?.format ?? "generic-jsonl");
  if (s && source !== "sqlite" && source !== "http" && !FORMAT_NAMES.includes(formatName) && formatName !== "custom") {
    errors.push(`sessions.format 未知：${formatName}（可用：${FORMAT_NAMES.join(", ")}, custom）`);
  }
  if (s && (source === "jsonl" || source === "json") && !s.glob && !s.dir) {
    errors.push("sessions 需要 glob 或 dir");
  }
  if (s && source === "sqlite" && !s.db) errors.push("sessions 需要 db（sqlite）");
  if (s && source === "http" && !s.url) errors.push("sessions 需要 url（http）");

  const push = String(m.push ?? "poll");
  if (!["watch", "poll", "hook"].includes(push)) errors.push(`push 必须是 watch | poll | hook（收到 ${push}）`);

  const reply = m.reply && typeof m.reply === "object" ? m.reply : null;
  if (reply) {
    if (!Array.isArray(reply.command) || reply.command.length === 0) {
      errors.push("reply.command 必须是参数数组（不给 shell 拼串）");
    } else if (reply.command.some((x) => typeof x !== "string")) {
      errors.push("reply.command 的每一项都必须是字符串");
    }
    if (reply.cwd != null && typeof reply.cwd !== "string") errors.push("reply.cwd 必须是字符串");
  }

  return {
    ok: errors.length === 0,
    errors,
    manifest: {
      id, name, icon,
      detect: { bin: detectBins, paths: detectPaths },
      sessions: s ? {
        source,
        glob: s.glob ? expandHome(String(s.glob), HOME) : null,
        dir: s.dir ? expandHome(String(s.dir), HOME) : null,
        db: s.db ? expandHome(String(s.db), HOME) : null,
        url: s.url ? String(s.url) : null,
        query: s.query ?? null,
        format: formatName,
        format_options: s.format_options || s.formatOptions || null,
      } : null,
      push,
      reply: reply ? {
        command: reply.command.map(String),
        cwd: reply.cwd ? String(reply.cwd) : null,
        timeout_ms: Number(reply.timeout_ms || reply.timeoutMs || 30_000),
      } : null,
      models: m.models && typeof m.models === "object" ? {
        file: m.models.file ? expandHome(String(m.models.file), HOME) : null,
        command: Array.isArray(m.models.command) ? m.models.command.map(String) : null,
      } : null,
      source_file: file,
      raw: m,
    },
  };
}

// ---- 回复命令：参数数组 spawn，占位符整体替换 ---------------------------

function substitute(arg, vars) {
  const whole = /^\{([a-z_]+)\}$/.exec(arg);
  if (whole) {
    const key = whole[1];
    if (!(key in vars)) throw new Error(`未知占位符 {${key}}`);
    return String(vars[key] ?? "");
  }
  // 非整体占位符：只做文本内替换，仍然不经过 shell
  return arg.replace(/\{([a-z_]+)\}/g, (_, k) => String(vars[k] ?? ""));
}

export function buildReplyCommand(cmd, vars) {
  return cmd.map((a) => substitute(String(a), vars));
}

export function runCommand(command, { cwd = null, timeoutMs = 30_000, input = null } = {}) {
  return new Promise((resolve) => {
    let child;
    try {
      child = spawn(command[0], command.slice(1), {
        cwd: cwd && fs.existsSync(cwd) ? cwd : undefined,
        stdio: ["ignore", "pipe", "pipe"],
        shell: false,          // 关键：不走 shell
        env: { ...process.env },
      });
    } catch (err) {
      resolve({ ok: false, error: `启动失败: ${err.message}` });
      return;
    }
    let out = "";
    let errOut = "";
    const timer = setTimeout(() => {
      try { child.kill("SIGKILL"); } catch { /* 忽略 */ }
      resolve({ ok: false, error: `超时 ${timeoutMs}ms` });
    }, timeoutMs);
    if (timer.unref) timer.unref();
    child.stdout?.on("data", (d) => { out += d.toString(); });
    child.stderr?.on("data", (d) => { errOut += d.toString(); });
    child.on("error", (err) => {
      clearTimeout(timer);
      resolve({ ok: false, error: err.message });
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      resolve({ ok: code === 0, code, stdout: out.slice(-8000), stderr: errOut.slice(-8000) });
    });
    if (input != null) {
      try { child.stdin?.end(String(input)); } catch { /* 忽略 */ }
    }
  });
}

// ---- 清单 → Scanner -----------------------------------------------------

function readModels(spec) {
  if (!spec) return [];
  if (spec.file) {
    try {
      const j = JSON.parse(fs.readFileSync(spec.file, "utf8"));
      const list = [];
      for (const [pid, prov] of Object.entries(j?.provider || {})) {
        const ids = prov?.models && typeof prov.models === "object" ? Object.keys(prov.models) : [];
        for (const mid of ids) list.push({ id: `${pid}/${mid}`, provider: pid, model: mid });
      }
      const def = typeof j?.model === "string" ? j.model : null;
      return { models: list, defaultModel: def, source: "file" };
    } catch {
      return { models: [], defaultModel: null, source: "none" };
    }
  }
  return { models: [], defaultModel: null, source: "none" };
}

// 清单里的 sqlite 来源：db + query（列名即字段名）
function querySessions(m, limit = 200) {
  const dbPath = m.sessions?.db;
  const sql = m.sessions?.query;
  if (!dbPath || !sql) return [];
  let db;
  try {
    db = new DatabaseSync(dbPath, { readOnly: true, timeout: 2000 });
    const rows = db.prepare(`${sql}`).all();
    return rows.slice(0, limit).map((r) => ({
      session_id: String(r.session_id ?? r.id ?? ""),
      source: m.id,
      title: r.title != null ? String(r.title) : "",
      title_source: "agent",
      last_ts: r.last_ts != null
        ? new Date(Number(r.last_ts) < 1e12 ? Number(r.last_ts) * 1000 : Number(r.last_ts)).toISOString()
        : null,
      native_state: null,
      cwd: r.cwd != null ? String(r.cwd) : null,
      model: r.model != null ? String(r.model) : null,
    })).filter((r) => r.session_id);
  } catch {
    return [];
  } finally {
    try { db?.close(); } catch { /* 忽略 */ }
  }
}

function buildScanner(m) {
  const format = m.sessions ? formatFor(m.sessions.format, m.sessions.format_options) : null;
  const cache = { at: 0, rows: [] };

  function filesNow() {
    if (Date.now() - cache.at < 20_000 && cache.rows.length) return cache.rows;
    const files = m.sessions?.glob ? expandGlob(m.sessions.glob) : [];
    cache.rows = files;
    cache.at = Date.now();
    return files;
  }

  const scanner = {
    id: m.id,
    name: m.name,
    icon: m.icon,
    custom: true,
    manifest: m,

    pushSignal: m.push,

    get capabilities() {
      return { reply: Boolean(m.reply), sessions: true };
    },

    async detect() {
      const bits = [];
      let installed = false;
      for (const p of m.detect.paths) {
        if (fs.existsSync(p)) { installed = true; bits.push(p); }
      }
      if (!installed) {
        for (const bin of m.detect.bin) {
          const found = (process.env.PATH || "").split(path.delimiter)
            .map((d) => path.join(d, bin))
            .find((p) => { try { fs.accessSync(p, fs.constants.X_OK); return true; } catch { return false; } });
          if (found) { installed = true; bits.push(found); break; }
        }
      }
      if (!installed && m.sessions?.glob) {
        // 有会话文件也算装上（用户可能只跑过 CLI）
        installed = filesNow().length > 0;
        if (installed) bits.push(`${filesNow().length} files`);
      }
      return { installed, version: null, detail: bits.join(" · ") || null };
    },

    async listSessions({ limit = 200 } = {}) {
      // sqlite 清单：直接跑 query，列名按 session_id / title / cwd / model / last_ts 取
      if (m.sessions?.source === "sqlite") {
        return querySessions(m, limit);
      }
      const files = filesNow().slice(0, Math.max(limit, 50) * 2);
      if (!format || !files.length) return [];
      const rows = format.listFiles(files, fs) || [];
      rows.sort((a, b) => ((a.last_ts || "") < (b.last_ts || "") ? 1 : -1));
      return rows.slice(0, limit).map((r) => ({
        session_id: r.session_id,
        source: m.id,
        title: r.title || "",
        title_source: r.title_source || "first_prompt",
        last_ts: r.last_ts,
        native_state: null,
        cwd: r.cwd,
        model: r.model,
        path: r.path,
      }));
    },

    meta(sessionId, cwdHint) {
      const files = filesNow();
      const hit = (format ? format.listFiles(files, fs) : []).find((r) => r.session_id === sessionId);
      return {
        cwd: cwdHint || hit?.cwd || null,
        nativeTitle: hit?.title || "",
        titleSource: hit?.title_source || "first_prompt",
        firstPrompt: "",
        branch: hit?.branch || null,
        model: hit?.model || null,
      };
    },

    readItems(sessionId, { limit = 100 } = {}) {
      const files = filesNow();
      const hit = (format ? format.listFiles(files, fs) : []).find((r) => r.session_id === sessionId);
      if (!hit || !format?.readItems) return [];
      return format.readItems(hit.path, fs, { limit });
    },

    isEmptySession(sessionId) {
      const files = filesNow();
      const hit = (format ? format.listFiles(files, fs) : []).find((r) => r.session_id === sessionId);
      if (!hit || !format?.hasConversation) return false;
      return !format.hasConversation(hit.path, fs);
    },

    async reply(sessionId, text, opts = {}) {
      if (!m.reply) return { ok: false, error: "该适配器未声明 reply" };
      const files = filesNow();
      const hit = (format ? format.listFiles(files, fs) : []).find((r) => r.session_id === sessionId);
      let command;
      try {
        command = buildReplyCommand(m.reply.command, {
          session_id: sessionId,
          text: String(text ?? ""),
          cwd: opts.cwd || hit?.cwd || "",
          title: hit?.title || "",
        });
      } catch (err) {
        return { ok: false, error: err.message };
      }
      const cwd = m.reply.cwd ? substitute(m.reply.cwd, {
        session_id: sessionId, text: String(text ?? ""), cwd: hit?.cwd || "",
      }) : null;
      const res = await runCommand(command, { cwd, timeoutMs: m.reply.timeout_ms });
      return res.ok
        ? { ok: true, via: "command", code: res.code }
        : { ok: false, error: res.error || res.stderr || `退出码 ${res.code}` };
    },

    async models() {
      return readModels(m.models);
    },
  };
  return scanner;
}

// ---- 加载 ---------------------------------------------------------------

export function loadManifests(dir = manifestDir()) {
  const out = [];
  const errors = [];
  let entries = [];
  try { entries = fs.readdirSync(dir); } catch { return { scanners: [], errors, dir }; }
  for (const name of entries.sort()) {
    if (!/\.(ya?ml)$/i.test(name)) continue;
    const file = path.join(dir, name);
    try {
      const raw = parseYaml(fs.readFileSync(file, "utf8"));
      const v = validateManifest(raw, file);
      if (!v.ok) {
        errors.push({ file, id: raw?.id || name, errors: v.errors });
        continue;
      }
      out.push(buildScanner(v.manifest));
    } catch (err) {
      errors.push({ file, id: name, errors: [err.message] });
    }
  }
  return { scanners: out, errors, dir };
}

export function loadManifestsFromFiles(files) {
  const out = [];
  const errors = [];
  for (const file of files) {
    try {
      const v = validateManifest(parseYaml(fs.readFileSync(file, "utf8")), file);
      if (!v.ok) { errors.push({ file, id: path.basename(file), errors: v.errors }); continue; }
      out.push(buildScanner(v.manifest));
    } catch (err) {
      errors.push({ file, id: path.basename(file), errors: [err.message] });
    }
  }
  return { scanners: out, errors };
}

export { buildScanner };
