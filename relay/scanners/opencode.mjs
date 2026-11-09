// Homing relay · scanner: opencode
// 数据源 1（始终可用）：~/.local/share/opencode/opencode.db（$OPENCODE_DB 可覆盖），只读。
// 数据源 2（可选）：`opencode serve` 的 HTTP 接口（默认 127.0.0.1:4096，HTTP Basic 鉴权）。
//   有 serve 时：事件流 GET /event 做推送、reply 走 POST /session/:id/prompt_async。
//   没有 serve 时：退回 60s 轮询数据库，reply 不支持（capabilities.reply=false）。
// 依据：https://docs.opencode.ai/docs/server/ 与 /docs/sdk/（2026-11 版）。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";
import * as oc from "./lib/opencode-db.mjs";

let ctx = {};                       // { log, emitEvent? }
export function init(c) {
  ctx = c || {};
  startServeWatch();
}

// ---- 路径 ---------------------------------------------------------------

function dataDir() {
  return process.env.XDG_DATA_HOME
    ? path.join(process.env.XDG_DATA_HOME, "opencode")
    : path.join(os.homedir(), ".local", "share", "opencode");
}

export function dbPath() {
  const override = process.env.OPENCODE_DB;
  const base = dataDir();
  if (override && override !== ":memory:") {
    return path.isAbsolute(override) ? override : path.join(base, override);
  }
  return path.join(base, "opencode.db");
}

function configPath() {
  const base = process.env.XDG_CONFIG_HOME
    ? path.join(process.env.XDG_CONFIG_HOME, "opencode")
    : path.join(os.homedir(), ".config", "opencode");
  return path.join(base, "opencode.json");
}

// serve 的 HTTP Basic 密码：opencode 自己存在 service.json 里
function servicePassword() {
  try {
    const p = path.join(path.dirname(configPath()), "service.json");
    const j = JSON.parse(fs.readFileSync(p, "utf8"));
    return typeof j.password === "string" ? j.password : null;
  } catch {
    return null;
  }
}

// ---- serve 探测 ---------------------------------------------------------

const SERVE_CANDIDATES = () => {
  const list = [];
  if (process.env.OPENCODE_SERVER_URL) list.push(process.env.OPENCODE_SERVER_URL.replace(/\/+$/, ""));
  list.push("http://127.0.0.1:4096");
  return list;
};

const serveState = { url: null, version: null, at: 0 };
const SERVE_TTL_MS = 15_000;

function basicAuth() {
  const pw = process.env.OPENCODE_SERVER_PASSWORD || servicePassword();
  if (!pw) return {};
  const user = process.env.OPENCODE_SERVER_USERNAME || "opencode";
  return { Authorization: `Basic ${Buffer.from(`${user}:${pw}`).toString("base64")}` };
}

async function probeServe(force = false) {
  if (!force && Date.now() - serveState.at < SERVE_TTL_MS) return serveState.url;
  serveState.at = Date.now();
  serveState.url = null;
  serveState.version = null;
  for (const base of SERVE_CANDIDATES()) {
    try {
      const res = await fetch(`${base}/global/health`, {
        headers: { ...basicAuth() },
        signal: AbortSignal.timeout(800),
      });
      if (res.ok) {
        const j = await res.json().catch(() => ({}));
        serveState.url = base;
        serveState.version = j?.version || null;
        break;
      }
    } catch {
      // 下一个候选
    }
  }
  return serveState.url;
}

export function serveUrl() {
  return serveState.url;
}

// ---- 检测 ---------------------------------------------------------------

export async function detect() {
  const p = dbPath();
  const installed = fs.existsSync(p);
  const info = installed ? oc.probe(p) : { ok: false, sessions: 0 };
  await probeServe();
  const served = Boolean(serveState.url);
  const version = serveState.version || null;
  const bits = [];
  if (info.ok) bits.push(`${info.sessions} sessions`);
  if (served) bits.push(`serve ${serveState.url}`);
  return {
    installed: installed || served,
    version,
    detail: bits.join(" · ") || null,
  };
}

// ---- 会话 ---------------------------------------------------------------

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

// ---- 回复注入 -----------------------------------------------------------
// 只在 serve 可达时成立：POST /session/:id/prompt_async { parts:[{type:"text",text}] } → 204
// 不走同步的 /session/:id/message，因为那会等模型跑完，可能几分钟。

export async function reply(sessionId, text, opts = {}) {
  const base = await probeServe();
  if (!base) {
    return { ok: false, error: "opencode serve 未运行，无法注入（请先 `opencode serve`）" };
  }
  const body = {
    parts: [{ type: "text", text: String(text || "") }],
  };
  if (opts.model && typeof opts.model === "string") {
    const [providerID, ...rest] = opts.model.split("/");
    if (providerID && rest.length) body.model = { providerID, modelID: rest.join("/") };
  }
  if (opts.noReply === true) body.noReply = true;
  try {
    const res = await fetch(`${base}/session/${encodeURIComponent(sessionId)}/prompt_async`, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...basicAuth() },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(10_000),
    });
    if (res.status === 204 || res.ok) return { ok: true, via: "serve" };
    const detail = await res.text().catch(() => "");
    return { ok: false, error: `opencode serve 返回 ${res.status}${detail ? `: ${detail.slice(0, 200)}` : ""}` };
  } catch (err) {
    return { ok: false, error: `注入失败: ${err.message}` };
  }
}

// ---- 模型列表 -----------------------------------------------------------
// 有 serve：GET /config/providers（权威）；没有：config 的 provider + models.dev 目录缓存（kv 表）。

function modelsFromConfig() {
  const out = [];
  try {
    const cfg = JSON.parse(fs.readFileSync(configPath(), "utf8"));
    const providers = cfg.provider && typeof cfg.provider === "object" ? cfg.provider : {};
    const catalog = catalogFromDB();
    for (const [pid, prov] of Object.entries(providers)) {
      const own = prov?.models && typeof prov.models === "object" ? Object.keys(prov.models) : [];
      const known = own.length ? own : Object.keys(catalog?.[pid]?.models || {});
      for (const mid of known) {
        out.push({ id: `${pid}/${mid}`, provider: pid, model: mid });
      }
    }
    const def = typeof cfg.model === "string" ? cfg.model : null;
    const small = typeof cfg.small_model === "string" ? cfg.small_model : null;
    return { models: out, defaultModel: def || (small || null) };
  } catch {
    return { models: out, defaultModel: null };
  }
}

function catalogFromDB() {
  const p = dbPath();
  if (!fs.existsSync(p)) return null;
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

export async function models() {
  const base = await probeServe();
  if (base) {
    try {
      const res = await fetch(`${base}/config/providers`, {
        headers: { ...basicAuth() },
        signal: AbortSignal.timeout(1500),
      });
      if (res.ok) {
        const j = await res.json();
        const list = [];
        for (const p of j?.providers || []) {
          const pid = p.id || p.providerID;
          for (const mid of Object.keys(p.models || {})) {
            list.push({ id: `${pid}/${mid}`, provider: pid, model: mid });
          }
        }
        if (list.length) {
          const def = j?.default && typeof j.default === "object" ? Object.values(j.default)[0] : null;
          return { models: list, defaultModel: def || null, source: "serve" };
        }
      }
    } catch {
      // 落到本地配置
    }
  }
  const local = modelsFromConfig();
  return { ...local, source: "config" };
}

// ---- 事件流（serve 时）---------------------------------------------------

let sseAbort = null;
let watchTimer = null;
let lastEventAt = 0;

function relayBase() {
  const port = Number(process.env.PORT || 18787);
  return `http://127.0.0.1:${port}`;
}

// 把 opencode 的事件转成 relay 的通用 hook 事件（/hook/generic 只接受回环来源）
async function forwardToRelay(event) {
  const source = "opencode";
  try {
    await fetch(`${relayBase()}/hook/generic`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(event),
      signal: AbortSignal.timeout(2000),
    });
  } catch (err) {
    ctx.log?.(`opencode 事件转发失败: ${err.message}`);
  }
}

function classify(type, props) {
  const t = String(type || props?.type || "");
  if (/permission/i.test(t)) return "permission";
  if (/idle|completed|finish|done/i.test(t)) return "reply";
  if (/error/i.test(t)) return "error";
  return null;
}

async function readSSE(base) {
  if (sseAbort) return;
  sseAbort = new AbortController();
  try {
    const res = await fetch(`${base}/event`, {
      headers: { Accept: "text/event-stream", ...basicAuth() },
      signal: sseAbort.signal,
    });
    if (!res.ok || !res.body) throw new Error(`event stream ${res.status}`);
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      let idx;
      while ((idx = buf.indexOf("\n\n")) >= 0) {
        const chunk = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        for (const line of chunk.split("\n")) {
          if (!line.startsWith("data:")) continue;
          const raw = line.slice(5).trim();
          if (!raw || raw === "[DONE]") continue;
          let ev;
          try { ev = JSON.parse(raw); } catch { continue; }
          lastEventAt = Date.now();
          const kind = classify(ev?.type, ev?.properties);
          const props = ev?.properties || {};
          const sessionId = props.sessionID || props.session_id || props.info?.sessionID || null;
          if (!kind || !sessionId) continue;
          await forwardToRelay({
            source: "opencode",
            kind,
            session_id: sessionId,
            cwd: props.info?.directory || null,
            title: props.info?.title || null,
            text: props.part?.text || props.message || null,
          });
        }
      }
    }
  } catch (err) {
    if (err.name !== "AbortError") {
      ctx.log?.(`opencode 事件流断开: ${err.message}`);
    }
  } finally {
    sseAbort = null;
  }
}

// 每 30s 检查一次 serve：出现了就订阅，消失就断开
function startServeWatch() {
  if (watchTimer) return;
  watchTimer = setInterval(async () => {
    const base = await probeServe(true);
    if (base && !sseAbort) readSSE(base).catch(() => {});
    if (!base && sseAbort) {
      try { sseAbort.abort(); } catch { /* 忽略 */ }
      sseAbort = null;
    }
  }, 30_000);
  if (watchTimer.unref) watchTimer.unref();
  probeServe(true).then((base) => {
    if (base) readSSE(base).catch(() => {});
  }).catch(() => {});
}

export function stats() {
  return {
    serve: serveState.url,
    sse: Boolean(sseAbort),
    last_event_at: lastEventAt ? new Date(lastEventAt).toISOString() : null,
  };
}

// ---- 注册 ---------------------------------------------------------------
// pushSignal / capabilities 是 getter：有 serve 才报 http_events / reply=true。

export default {
  id: "opencode",
  name: "OpenCode",
  icon: "opencode",
  init,
  detect,
  meta,
  listSessions,
  readItems,
  isEmptySession,
  reply,
  models,
  stats,
  get pushSignal() {
    return serveState.url ? "http_events" : "poll";
  },
  get capabilities() {
    return { reply: Boolean(serveState.url), sessions: true };
  },
};
